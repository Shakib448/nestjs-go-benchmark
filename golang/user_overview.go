package main

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type UserOverviewHandler struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

type User struct {
	ID     string `json:"id"`
	Plan   string `json:"plan"`
	Region string `json:"region"`
	Status string `json:"status"`
}

type Cart struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	CartTotal float64   `json:"cart_total"`
	CartItems int       `json:"cart_items"`
}

type Order struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Total      float64   `json:"total"`
	CreatedAt  time.Time `json:"created_at"`
	ItemsCount int       `json:"items_count"`
}

type Product struct {
	ID        string  `json:"id"`
	SKU       string  `json:"sku"`
	Price     float64 `json:"price"`
	Available int     `json:"available"`
}

type Derived struct {
	UserSegment    string   `json:"user_segment"`
	CartAgeSeconds *int     `json:"cart_age_seconds"`
	TopProducts    []string `json:"top_products"`
}

type UserOverviewResponse struct {
	User     *User     `json:"user"`
	Cart     *Cart     `json:"cart"`
	Orders   []Order   `json:"orders"`
	Products []Product `json:"products"`
	Derived  Derived   `json:"derived"`
}

func NewUserOverviewHandler(
	db *pgxpool.Pool,
	rdb *redis.Client,
) *UserOverviewHandler {
	return &UserOverviewHandler{db: db, rdb: rdb}
}

func (h *UserOverviewHandler) GetUserOverview(c *fiber.Ctx) error {
	ctx := c.Context()
	userID := c.Params("userId")
	categoryID := c.Query("categoryId")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	// 1) Validate user exists (DB light read or cached)
	user, err := h.getCachedUser(ctx, userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": err.Error()})
	}
	if user == nil {
		user, err = h.getUserFromDB(ctx, userID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).
				JSON(fiber.Map{"error": err.Error()})
		}
		if user == nil {
			return c.Status(fiber.StatusNotFound).
				JSON(fiber.Map{"error": "User not found"})
		}
		h.cacheUser(ctx, userID, user)
	}

	// 2) Check summary cache (short TTL)
	summaryKey := "cache:user:" + userID + ":summary:" + categoryID + ":" + strconv.Itoa(
		page,
	) + ":" + strconv.Itoa(
		limit,
	)
	if categoryID == "" {
		summaryKey = "cache:user:" + userID + ":summary:all:" + strconv.Itoa(
			page,
		) + ":" + strconv.Itoa(
			limit,
		)
	}

	cached, err := h.rdb.Get(ctx, summaryKey).Result()
	if err == nil && cached != "" {
		h.rdb.Incr(ctx, "metrics:get_overview_hits")
		var response UserOverviewResponse
		json.Unmarshal([]byte(cached), &response)
		return c.JSON(response)
	}

	// 3) Complex DB read (joins + aggregation + pagination)
	orders, err := h.getRecentOrders(ctx, userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": err.Error()})
	}

	cart, err := h.getCurrentCart(ctx, userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": err.Error()})
	}

	products, err := h.getRecommendedProducts(ctx, categoryID, page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": err.Error()})
	}

	// 4) Compute derived fields (CPU work)
	var orderTotalSum float64
	for _, o := range orders {
		orderTotalSum += o.Total
	}

	derived := Derived{
		UserSegment: computeSegment(user.Plan, user.Region, orderTotalSum),
		TopProducts: make([]string, 0),
	}

	if cart != nil {
		age := int(time.Since(cart.UpdatedAt).Seconds())
		derived.CartAgeSeconds = &age
	}

	for i, p := range products {
		if i >= 3 {
			break
		}
		derived.TopProducts = append(derived.TopProducts, p.ID)
	}

	response := UserOverviewResponse{
		User:     user,
		Cart:     cart,
		Orders:   orders,
		Products: products,
		Derived:  derived,
	}

	// 5) Store summary cache, plus some extra redis ops
	responseJSON, _ := json.Marshal(response)
	h.rdb.SetEx(ctx, summaryKey, string(responseJSON), 30*time.Second)
	h.rdb.SAdd(ctx, "metrics:active_users", userID)
	h.rdb.Expire(ctx, "metrics:active_users", 3600*time.Second)

	return c.JSON(response)
}

func (h *UserOverviewHandler) getCachedUser(
	ctx context.Context,
	userID string,
) (*User, error) {
	cached, err := h.rdb.Get(ctx, "cache:user:"+userID).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var user User
	json.Unmarshal([]byte(cached), &user)
	return &user, nil
}

func (h *UserOverviewHandler) getUserFromDB(
	ctx context.Context,
	userID string,
) (*User, error) {
	row := h.db.QueryRow(
		ctx,
		`SELECT id, plan, region, status FROM users WHERE id = $1 AND status = 'active'`,
		userID,
	)

	var user User
	err := row.Scan(&user.ID, &user.Plan, &user.Region, &user.Status)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (h *UserOverviewHandler) cacheUser(
	ctx context.Context,
	userID string,
	user *User,
) {
	data, _ := json.Marshal(user)
	h.rdb.SetEx(ctx, "cache:user:"+userID, string(data), 120*time.Second)
}

func (h *UserOverviewHandler) getRecentOrders(
	ctx context.Context,
	userID string,
) ([]Order, error) {
	rows, err := h.db.Query(ctx, `
		SELECT o.id, o.status, o.total, o.created_at, COUNT(oi.product_id)::int as items_count
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		WHERE o.user_id = $1
		GROUP BY o.id
		ORDER BY o.created_at DESC
		LIMIT 10`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		err := rows.Scan(
			&o.ID,
			&o.Status,
			&o.Total,
			&o.CreatedAt,
			&o.ItemsCount,
		)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
}

func (h *UserOverviewHandler) getCurrentCart(
	ctx context.Context,
	userID string,
) (*Cart, error) {
	row := h.db.QueryRow(ctx, `
		SELECT c.id, c.status, c.updated_at,
			   COALESCE(SUM(ci.qty * ci.unit_price), 0)::decimal AS cart_total,
			   COALESCE(SUM(ci.qty), 0)::int AS cart_items
		FROM carts c
		LEFT JOIN cart_items ci ON ci.cart_id = c.id
		WHERE c.user_id = $1 AND c.status = 'open'
		GROUP BY c.id
		LIMIT 1`, userID)

	var cart Cart
	err := row.Scan(
		&cart.ID,
		&cart.Status,
		&cart.UpdatedAt,
		&cart.CartTotal,
		&cart.CartItems,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &cart, nil
}

func (h *UserOverviewHandler) getRecommendedProducts(
	ctx context.Context,
	categoryID string,
	page, limit int,
) ([]Product, error) {
	offset := (page - 1) * limit

	var rows pgx.Rows
	var err error

	if categoryID == "" {
		rows, err = h.db.Query(ctx, `
			SELECT p.id, p.sku, p.price,
				   COALESCE(SUM(i.available_qty - i.reserved_qty), 0)::int as available
			FROM products p
			LEFT JOIN inventory i ON i.product_id = p.id
			WHERE p.status = 'active'
			GROUP BY p.id
			ORDER BY available DESC, p.id DESC
			OFFSET $1 LIMIT $2`, offset, limit)
	} else {
		rows, err = h.db.Query(ctx, `
			SELECT p.id, p.sku, p.price,
				   COALESCE(SUM(i.available_qty - i.reserved_qty), 0)::int as available
			FROM products p
			LEFT JOIN inventory i ON i.product_id = p.id
			WHERE p.status = 'active' AND p.category_id = $1
			GROUP BY p.id
			ORDER BY available DESC, p.id DESC
			OFFSET $2 LIMIT $3`, categoryID, offset, limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		err := rows.Scan(&p.ID, &p.SKU, &p.Price, &p.Available)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, nil
}

func computeSegment(plan, region string, totalSpend float64) string {
	// Simulate some CPU work
	str := plan + ":" + region + ":" + strconv.FormatFloat(
		totalSpend,
		'f',
		2,
		64,
	)
	hash := 0
	for _, c := range str {
		hash = (hash << 5) - hash + int(c)
	}

	if plan == "enterprise" || totalSpend > 10000 {
		return "vip"
	}
	if plan == "premium" || totalSpend > 5000 {
		return "premium"
	}
	if plan == "basic" || totalSpend > 1000 {
		return "standard"
	}
	return "basic"
}
