package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type CheckoutHandler struct {
	db                *pgxpool.Pool
	rdb               *redis.Client
	warehouseByRegion map[string]string
}

type CheckoutRequest struct {
	UserID     string         `json:"userId"`
	CartID     string         `json:"cartId"`
	Items      []CheckoutItem `json:"items"`
	Coupon     string         `json:"coupon"`
	PaymentRef string         `json:"paymentRef"`
}

type CheckoutItem struct {
	ProductID string `json:"productId"`
	Qty       int    `json:"qty"`
}

type CheckoutResponse struct {
	OrderID string  `json:"orderId"`
	Status  string  `json:"status"`
	Total   float64 `json:"total"`
}

type CartItemDB struct {
	ProductID string
	Qty       int
	UnitPrice float64
	Status    string
}

type CouponDB struct {
	Code      string
	Type      string
	Value     float64
	MaxUses   *int
	UsedCount int
	StartsAt  time.Time
	EndsAt    time.Time
}

func NewCheckoutHandler(db *pgxpool.Pool, rdb *redis.Client) *CheckoutHandler {
	return &CheckoutHandler{
		db:  db,
		rdb: rdb,
		warehouseByRegion: map[string]string{
			"us-east":      "11111111-1111-1111-1111-111111111111",
			"us-west":      "22222222-2222-2222-2222-222222222222",
			"eu-west":      "33333333-3333-3333-3333-333333333333",
			"ap-southeast": "44444444-4444-4444-4444-444444444444",
		},
	}
}

func (h *CheckoutHandler) Checkout(c *fiber.Ctx) error {
	ctx := c.Context()

	var req CheckoutRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": err.Error()})
	}

	// Validate required fields
	if req.UserID == "" || req.CartID == "" || req.PaymentRef == "" {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "userId, cartId, and paymentRef are required"})
	}
	if len(req.Items) == 0 {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "items are required"})
	}

	result, err := h.processCheckout(ctx, req)
	if err != nil {
		status := fiber.StatusInternalServerError
		switch err.Error() {
		case "Rate limit exceeded":
			status = fiber.StatusTooManyRequests
		case "Checkout in progress":
			status = fiber.StatusConflict
		case "Cart not found or not open",
			"Cart is empty",
			"Invalid or expired coupon",
			"Coupon already used":
			status = fiber.StatusBadRequest
		case "Insufficient inventory":
			status = fiber.StatusConflict
		}
		return c.Status(status).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(result)
}

func (h *CheckoutHandler) processCheckout(
	ctx context.Context,
	req CheckoutRequest,
) (*CheckoutResponse, error) {
	idempotencyKey := "idem:checkout:" + req.PaymentRef

	// 0) Idempotency check (Redis)
	existing, err := h.rdb.Get(ctx, idempotencyKey).Result()
	if err == nil && existing != "" {
		var resp CheckoutResponse
		json.Unmarshal([]byte(existing), &resp)
		return &resp, nil
	}

	// 1) Rate limit (Redis)
	currentMinute := time.Now().Unix() / 60
	rlKey := fmt.Sprintf("rl:user:%s:checkout:%d", req.UserID, currentMinute)
	count, err := h.rdb.Incr(ctx, rlKey).Result()
	if err != nil {
		return nil, err
	}
	if count == 1 {
		h.rdb.Expire(ctx, rlKey, 90*time.Second)
	}
	if count > 10 {
		return nil, errors.New("Rate limit exceeded")
	}

	// 2) Distributed lock (Redis)
	lockKey := "lock:checkout:" + req.UserID
	locked, err := h.rdb.SetNX(ctx, lockKey, "1", 5*time.Second).Result()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("Checkout in progress")
	}
	defer h.rdb.Del(ctx, lockKey)

	// Execute transaction
	result, err := h.executeCheckoutTransaction(ctx, req)
	if err != nil {
		return nil, err
	}

	// 5) Store idempotency response
	responseJSON, _ := json.Marshal(result)
	h.rdb.SetEx(ctx, idempotencyKey, string(responseJSON), 10*time.Minute)

	return result, nil
}

func (h *CheckoutHandler) executeCheckoutTransaction(
	ctx context.Context,
	req CheckoutRequest,
) (*CheckoutResponse, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 3.1) Validate cart ownership & open status (row lock)
	var cartID, cartStatus string
	err = tx.QueryRow(
		ctx,
		`SELECT id, status FROM carts WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		req.CartID,
		req.UserID,
	).Scan(&cartID, &cartStatus)
	if err != nil || cartStatus != "open" {
		return nil, errors.New("Cart not found or not open")
	}

	// 3.2) Load items from DB
	rows, err := tx.Query(ctx, `
		SELECT ci.product_id, ci.qty, ci.unit_price, p.status
		FROM cart_items ci
		JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = $1`, req.CartID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cartItems []CartItemDB
	for rows.Next() {
		var item CartItemDB
		err := rows.Scan(
			&item.ProductID,
			&item.Qty,
			&item.UnitPrice,
			&item.Status,
		)
		if err != nil {
			return nil, err
		}
		cartItems = append(cartItems, item)
	}
	if len(cartItems) == 0 {
		return nil, errors.New("Cart is empty")
	}

	// 3.3) Coupon validation + usage lock
	var discount float64
	if req.Coupon != "" {
		discount, err = h.processCoupon(
			ctx,
			tx,
			req.UserID,
			req.Coupon,
			cartItems,
		)
		if err != nil {
			return nil, err
		}
	}

	// 3.4) Inventory reservation (lock rows)
	warehouseID, err := h.getWarehouseForUser(ctx, tx, req.UserID)
	if err != nil {
		return nil, err
	}
	err = h.reserveInventory(ctx, tx, cartItems, warehouseID)
	if err != nil {
		return nil, err
	}

	// 3.5) Compute totals
	var subtotal float64
	for _, item := range cartItems {
		subtotal += float64(item.Qty) * item.UnitPrice
	}
	tax := computeTax(subtotal - discount)
	shipping := computeShipping(subtotal, len(cartItems))
	total := maxFloat(0, subtotal-discount+tax+shipping)

	// 3.6) Create order + items
	orderID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO orders(id, user_id, status, subtotal, discount, tax, shipping, total, created_at)
		VALUES($1, $2, 'pending', $3, $4, $5, $6, $7, NOW())`,
		orderID, req.UserID, subtotal, discount, tax, shipping, total)
	if err != nil {
		return nil, err
	}

	// Insert order items
	for _, item := range cartItems {
		_, err = tx.Exec(
			ctx,
			`
			INSERT INTO order_items(id, order_id, product_id, qty, unit_price)
			VALUES($1, $2, $3, $4, $5)`,
			uuid.New().
				String(),
			orderID,
			item.ProductID,
			item.Qty,
			item.UnitPrice,
		)
		if err != nil {
			return nil, err
		}
	}

	// 3.7) Mark cart closed
	_, err = tx.Exec(
		ctx,
		`UPDATE carts SET status = 'closed', updated_at = NOW() WHERE id = $1`,
		req.CartID,
	)
	if err != nil {
		return nil, err
	}

	// 3.8) Event log
	payload, _ := json.Marshal(map[string]interface{}{
		"orderId": orderID,
		"total":   total,
		"cartId":  req.CartID,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO events(user_id, type, payload_json, created_at)
		VALUES($1, 'ORDER_CREATED', $2, NOW())`,
		req.UserID, string(payload))
	if err != nil {
		return nil, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}

	// 4) Post-commit Redis work
	h.postCommitRedisOps(ctx, req.UserID, orderID, total)

	return &CheckoutResponse{
		OrderID: orderID,
		Status:  "pending",
		Total:   total,
	}, nil
}

func (h *CheckoutHandler) processCoupon(
	ctx context.Context,
	tx pgx.Tx,
	userID, couponCode string,
	cartItems []CartItemDB,
) (float64, error) {
	var coupon CouponDB
	err := tx.QueryRow(ctx, `
		SELECT code, type, value, max_uses, used_count, starts_at, ends_at
		FROM coupons WHERE code = $1 FOR UPDATE`, couponCode).
		Scan(&coupon.Code, &coupon.Type, &coupon.Value, &coupon.MaxUses, &coupon.UsedCount, &coupon.StartsAt, &coupon.EndsAt)
	if err != nil {
		return 0, errors.New("Invalid or expired coupon")
	}

	now := time.Now()
	if now.Before(coupon.StartsAt) || now.After(coupon.EndsAt) {
		return 0, errors.New("Invalid or expired coupon")
	}
	if coupon.MaxUses != nil && coupon.UsedCount >= *coupon.MaxUses {
		return 0, errors.New("Invalid or expired coupon")
	}

	// Check user usage
	var usedCount int
	err = tx.QueryRow(ctx, `
		SELECT used_count FROM user_coupon_usage
		WHERE user_id = $1 AND coupon_code = $2 FOR UPDATE`, userID, couponCode).Scan(&usedCount)
	if err == nil && usedCount >= 1 {
		return 0, errors.New("Coupon already used")
	}

	// Mark usage
	_, err = tx.Exec(ctx, `
		INSERT INTO user_coupon_usage(user_id, coupon_code, used_count)
		VALUES($1, $2, 1)
		ON CONFLICT(user_id, coupon_code)
		DO UPDATE SET used_count = user_coupon_usage.used_count + 1`, userID, couponCode)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(
		ctx,
		`UPDATE coupons SET used_count = used_count + 1 WHERE code = $1`,
		couponCode,
	)
	if err != nil {
		return 0, err
	}

	// Compute discount
	var subtotal float64
	for _, item := range cartItems {
		subtotal += float64(item.Qty) * item.UnitPrice
	}

	if coupon.Type == "percentage" {
		return subtotal * (coupon.Value / 100), nil
	}
	return coupon.Value, nil
}

func (h *CheckoutHandler) getWarehouseForUser(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) (string, error) {
	var region string
	err := tx.QueryRow(ctx, `SELECT region FROM users WHERE id = $1`, userID).
		Scan(&region)
	if err != nil {
		return h.warehouseByRegion["us-east"], nil
	}
	if wh, ok := h.warehouseByRegion[region]; ok {
		return wh, nil
	}
	return h.warehouseByRegion["us-east"], nil
}

func (h *CheckoutHandler) reserveInventory(
	ctx context.Context,
	tx pgx.Tx,
	cartItems []CartItemDB,
	warehouseID string,
) error {
	for _, item := range cartItems {
		var availableQty, reservedQty int
		err := tx.QueryRow(ctx, `
			SELECT available_qty, reserved_qty FROM inventory
			WHERE product_id = $1 AND warehouse_id = $2
			FOR UPDATE`, item.ProductID, warehouseID).Scan(&availableQty, &reservedQty)
		if err != nil {
			return errors.New("Insufficient inventory")
		}

		if availableQty-reservedQty < item.Qty {
			return errors.New("Insufficient inventory")
		}

		_, err = tx.Exec(ctx, `
			UPDATE inventory
			SET reserved_qty = reserved_qty + $1, updated_at = NOW()
			WHERE product_id = $2 AND warehouse_id = $3`, item.Qty, item.ProductID, warehouseID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *CheckoutHandler) postCommitRedisOps(
	ctx context.Context,
	userID, orderID string,
	total float64,
) {
	// Delete user summary cache keys
	keys, _ := h.rdb.Keys(ctx, "cache:user:"+userID+":summary:*").Result()
	if len(keys) > 0 {
		h.rdb.Del(ctx, keys...)
	}

	h.rdb.ZIncrBy(ctx, "leaderboard:top_buyers", total, userID)
	h.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "stream:order_events",
		Values: map[string]interface{}{
			"userId":  userID,
			"orderId": orderID,
			"total":   total,
		},
	})
}

func computeTax(amount float64) float64 {
	return float64(int(amount*0.08*100)) / 100
}

func computeShipping(subtotal float64, itemCount int) float64 {
	if subtotal > 100 {
		return 0
	}
	return 5.99 + float64(itemCount-1)*0.99
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
