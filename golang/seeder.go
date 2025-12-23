package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Configuration - 1M+ ORDERS, 3M+ ORDER ITEMS
const (
	TOTAL_USERS    = 100_000
	TOTAL_PRODUCTS = 10_000
	TOTAL_CARTS    = 50_000
	TOTAL_ORDERS   = 1_000_000 // 1M orders
	TOTAL_EVENTS   = 100_000

	BATCH_SIZE = 10_000 // Rows per batch
	WORKERS    = 20     // Concurrent workers
)

var (
	plans      = []string{"free", "basic", "premium", "enterprise"}
	regions    = []string{"us-east", "us-west", "eu-west", "ap-southeast"}
	statuses   = []string{"active", "active", "active", "active", "inactive"}
	orderStats = []string{"pending", "completed", "shipped", "delivered"}
	eventTypes = []string{
		"ORDER_CREATED",
		"ORDER_SHIPPED",
		"ORDER_DELIVERED",
		"CART_UPDATED",
		"COUPON_USED",
	}

	categoryIDs = []string{
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"cccccccc-cccc-cccc-cccc-cccccccccccc",
		"dddddddd-dddd-dddd-dddd-dddddddddddd",
	}

	warehouseIDs = []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
		"44444444-4444-4444-4444-444444444444",
	}
)

var totalInserted int64

func main() {
	start := time.Now()

	dbURL := buildDBURL()
	pool := connectDB(dbURL)
	defer pool.Close()

	log.Println("🚀 Starting data population...")
	log.Printf("🎯 Target: 1M orders + 3M+ order items (~4.5M total rows)\n\n")

	rand.Seed(time.Now().UnixNano())

	ctx, cancel := context.WithCancel(context.Background())
	go progressReporter(ctx)

	// Seed tables
	userIDs := seedUsers(pool)
	productIDs := seedProducts(pool)
	seedInventory(pool, productIDs)
	seedCoupons(pool)
	cartIDs := seedCarts(pool, userIDs)
	seedCartItems(pool, cartIDs, productIDs)
	orderIDs := seedOrders(pool, userIDs)
	seedOrderItems(pool, orderIDs, productIDs)
	seedEvents(pool, userIDs)

	cancel()

	elapsed := time.Since(start)
	printSummary(pool, elapsed)
}

func buildDBURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?pool_max_conns=30",
		getEnvKey("DB_USER", "postgres"),
		getEnvKey("DB_PASSWORD", "postgres"),
		getEnvKey("DB_HOST", "localhost"),
		getEnvKey("DB_PORT", "5432"),
		getEnvKey("DB_NAME", "loadtest"))
}

func connectDB(dbURL string) *pgxpool.Pool {
	config, _ := pgxpool.ParseConfig(dbURL)
	config.MaxConns = 30
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatalf("❌ DB connection failed: %v", err)
	}
	log.Println("✅ Database connected")
	return pool
}

func progressReporter(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count := atomic.LoadInt64(&totalInserted)
			log.Printf(
				"   ⏳ Progress: %.2fM rows...\n",
				float64(count)/1_000_000,
			)
		}
	}
}

func seedUsers(pool *pgxpool.Pool) []string {
	log.Println("📦 [1/9] Creating users...")
	userIDs := make([]string, TOTAL_USERS)
	for i := range userIDs {
		userIDs[i] = uuid.New().String()
	}

	parallelInsert(pool, TOTAL_USERS, func(start, end int) int64 {
		rows := make([][]interface{}, 0, end-start)
		for i := start; i < end; i++ {
			rows = append(rows, []interface{}{
				userIDs[i],
				plans[rand.Intn(4)],
				regions[rand.Intn(4)],
				statuses[rand.Intn(5)],
				randomTime(730),
			})
		}
		return copyRows(
			pool,
			"users",
			[]string{"id", "plan", "region", "status", "created_at"},
			rows,
		)
	})

	log.Printf("✅ Created %d users\n\n", TOTAL_USERS)
	return userIDs
}

func seedProducts(pool *pgxpool.Pool) []string {
	log.Println("📦 [2/9] Creating products...")
	productIDs := make([]string, TOTAL_PRODUCTS)
	rows := make([][]interface{}, 0, TOTAL_PRODUCTS)

	for i := 0; i < TOTAL_PRODUCTS; i++ {
		productIDs[i] = uuid.New().String()
		rows = append(rows, []interface{}{
			productIDs[i],
			fmt.Sprintf("SKU-%08d", i+1),
			10.0 + rand.Float64()*990.0,
			"active",
			categoryIDs[rand.Intn(4)],
		})
	}

	count := copyRows(
		pool,
		"products",
		[]string{"id", "sku", "price", "status", "category_id"},
		rows,
	)
	atomic.AddInt64(&totalInserted, count)
	log.Printf("✅ Created %d products\n\n", TOTAL_PRODUCTS)
	return productIDs
}

func seedInventory(pool *pgxpool.Pool, productIDs []string) {
	log.Println("📦 [3/9] Creating inventory...")
	rows := make([][]interface{}, 0, len(productIDs)*4)

	for _, pid := range productIDs {
		for _, wid := range warehouseIDs {
			rows = append(rows, []interface{}{
				pid, wid,
				100 + rand.Intn(5000),
				rand.Intn(50),
				time.Now().Add(-time.Duration(rand.Intn(30)) * 24 * time.Hour),
			})
		}
	}

	count := copyRows(
		pool,
		"inventory",
		[]string{
			"product_id",
			"warehouse_id",
			"available_qty",
			"reserved_qty",
			"updated_at",
		},
		rows,
	)
	atomic.AddInt64(&totalInserted, count)
	log.Printf("✅ Created %d inventory records\n\n", len(productIDs)*4)
}

func seedCoupons(pool *pgxpool.Pool) {
	log.Println("📦 [4/9] Creating coupons...")
	rows := [][]interface{}{
		{
			"WELCOME10",
			"percentage",
			10.0,
			1000000,
			0,
			time.Now(),
			time.Now().AddDate(1, 0, 0),
		},
		{
			"SAVE20",
			"percentage",
			20.0,
			500000,
			0,
			time.Now(),
			time.Now().AddDate(0, 6, 0),
		},
		{
			"FLAT50",
			"fixed",
			50.0,
			100000,
			0,
			time.Now(),
			time.Now().AddDate(0, 3, 0),
		},
		{
			"SUMMER25",
			"percentage",
			25.0,
			200000,
			0,
			time.Now(),
			time.Now().AddDate(0, 2, 0),
		},
		{
			"VIP30",
			"percentage",
			30.0,
			50000,
			0,
			time.Now(),
			time.Now().AddDate(1, 0, 0),
		},
	}

	for i := 1; i <= 100; i++ {
		t, v := "percentage", 5.0+rand.Float64()*25.0
		if rand.Intn(3) == 0 {
			t, v = "fixed", 10.0+rand.Float64()*90.0
		}
		rows = append(
			rows,
			[]interface{}{
				fmt.Sprintf("CODE%05d", i),
				t,
				v,
				10000 + rand.Intn(90000),
				0,
				time.Now(),
				time.Now().AddDate(1, 0, 0),
			},
		)
	}

	count := copyRows(
		pool,
		"coupons",
		[]string{
			"code",
			"type",
			"value",
			"max_uses",
			"used_count",
			"starts_at",
			"ends_at",
		},
		rows,
	)
	atomic.AddInt64(&totalInserted, count)
	log.Println("✅ Created coupons\n")
}

func seedCarts(pool *pgxpool.Pool, userIDs []string) []string {
	log.Println("📦 [5/9] Creating carts...")
	cartIDs := make([]string, TOTAL_CARTS)
	rows := make([][]interface{}, 0, TOTAL_CARTS)

	for i := 0; i < TOTAL_CARTS; i++ {
		cartIDs[i] = uuid.New().String()
		rows = append(rows, []interface{}{
			cartIDs[i],
			userIDs[rand.Intn(len(userIDs))],
			"open",
			randomTime(7),
		})
	}

	count := copyRows(
		pool,
		"carts",
		[]string{"id", "user_id", "status", "updated_at"},
		rows,
	)
	atomic.AddInt64(&totalInserted, count)
	log.Printf("✅ Created %d carts\n\n", TOTAL_CARTS)
	return cartIDs
}

func seedCartItems(pool *pgxpool.Pool, cartIDs []string, productIDs []string) {
	log.Println("📦 [6/9] Creating cart items...")
	var totalItems int64

	parallelInsert(pool, len(cartIDs), func(start, end int) int64 {
		rows := make([][]interface{}, 0)
		used := make(map[string]map[string]bool)

		for i := start; i < end; i++ {
			cid := cartIDs[i]
			used[cid] = make(map[string]bool)
			for j := 0; j < 1+rand.Intn(5); j++ {
				pid := productIDs[rand.Intn(len(productIDs))]
				if used[cid][pid] {
					continue
				}
				used[cid][pid] = true
				rows = append(
					rows,
					[]interface{}{
						uuid.New().String(),
						cid,
						pid,
						1 + rand.Intn(4),
						10.0 + rand.Float64()*500.0,
					},
				)
			}
		}
		return copyRows(
			pool,
			"cart_items",
			[]string{"id", "cart_id", "product_id", "qty", "unit_price"},
			rows,
		)
	})

	log.Printf("✅ Created cart items\n\n")
	_ = totalItems
}

func seedOrders(pool *pgxpool.Pool, userIDs []string) []string {
	log.Println("📦 [7/9] Creating 1M orders...")
	orderIDs := make([]string, TOTAL_ORDERS)
	for i := range orderIDs {
		orderIDs[i] = uuid.New().String()
	}

	parallelInsert(pool, TOTAL_ORDERS, func(start, end int) int64 {
		rows := make([][]interface{}, 0, end-start)
		for i := start; i < end; i++ {
			subtotal := 50.0 + rand.Float64()*1000.0
			discount := rand.Float64() * 50.0
			tax := subtotal * 0.08
			shipping := 0.0
			if subtotal < 100 {
				shipping = 9.99
			}
			rows = append(rows, []interface{}{
				orderIDs[i],
				userIDs[rand.Intn(len(userIDs))],
				orderStats[rand.Intn(4)],
				subtotal, discount, tax, shipping,
				subtotal - discount + tax + shipping,
				randomTime(365),
			})
		}
		return copyRows(
			pool,
			"orders",
			[]string{
				"id",
				"user_id",
				"status",
				"subtotal",
				"discount",
				"tax",
				"shipping",
				"total",
				"created_at",
			},
			rows,
		)
	})

	log.Printf("✅ Created %d orders\n\n", TOTAL_ORDERS)
	return orderIDs
}

func seedOrderItems(
	pool *pgxpool.Pool,
	orderIDs []string,
	productIDs []string,
) {
	log.Println("📦 [8/9] Creating 3M+ order items...")

	parallelInsert(pool, len(orderIDs), func(start, end int) int64 {
		rows := make([][]interface{}, 0, (end-start)*4)
		for i := start; i < end; i++ {
			oid := orderIDs[i]
			for j := 0; j < 1+rand.Intn(5); j++ { // 1-5 items, avg ~3
				rows = append(rows, []interface{}{
					uuid.New().String(),
					oid,
					productIDs[rand.Intn(len(productIDs))],
					1 + rand.Intn(3),
					10.0 + rand.Float64()*500.0,
				})
			}
		}
		return copyRows(
			pool,
			"order_items",
			[]string{"id", "order_id", "product_id", "qty", "unit_price"},
			rows,
		)
	})

	log.Println("✅ Created order items\n")
}

func seedEvents(pool *pgxpool.Pool, userIDs []string) {
	log.Println("📦 [9/9] Creating events...")

	parallelInsert(pool, TOTAL_EVENTS, func(start, end int) int64 {
		rows := make([][]interface{}, 0, end-start)
		for i := start; i < end; i++ {
			rows = append(rows, []interface{}{
				uuid.New().String(),
				userIDs[rand.Intn(len(userIDs))],
				eventTypes[rand.Intn(5)],
				fmt.Sprintf(
					`{"action":"event_%d","value":%d}`,
					i,
					rand.Intn(1000),
				),
				randomTime(90),
			})
		}
		return copyRows(
			pool,
			"events",
			[]string{"id", "user_id", "type", "payload_json", "created_at"},
			rows,
		)
	})

	log.Printf("✅ Created %d events\n\n", TOTAL_EVENTS)
}

// ============ HELPERS ============

func parallelInsert(
	pool *pgxpool.Pool,
	total int,
	fn func(start, end int) int64,
) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, WORKERS)

	for start := 0; start < total; start += BATCH_SIZE {
		end := start + BATCH_SIZE
		if end > total {
			end = total
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(s, e int) {
			defer wg.Done()
			defer func() { <-sem }()
			count := fn(s, e)
			atomic.AddInt64(&totalInserted, count)
		}(start, end)
	}
	wg.Wait()
}

func copyRows(
	pool *pgxpool.Pool,
	table string,
	cols []string,
	rows [][]interface{},
) int64 {
	count, err := pool.CopyFrom(
		context.Background(),
		pgx.Identifier{table},
		cols,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		log.Printf("❌ Error inserting into %s: %v", table, err)
	}
	return count
}

func printSummary(pool *pgxpool.Pool, elapsed time.Duration) {
	log.Println("========================================")
	log.Println("🎉 DATA POPULATION COMPLETE!")
	log.Println("========================================")

	total := atomic.LoadInt64(&totalInserted)
	log.Printf("Total rows: %d (%.2fM)\n", total, float64(total)/1_000_000)
	log.Printf("Time: %v\n", elapsed)
	log.Printf("Speed: %.0f rows/sec\n", float64(total)/elapsed.Seconds())

	log.Println("\n📊 Table Counts:")
	log.Println("----------------------------------------")
	tables := []string{
		"users",
		"products",
		"inventory",
		"carts",
		"cart_items",
		"orders",
		"order_items",
		"coupons",
		"events",
	}
	var sum int64
	for _, t := range tables {
		var c int64
		pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+t).Scan(&c)
		log.Printf("  %-15s %12d", t, c)
		sum += c
	}
	log.Println("----------------------------------------")
	log.Printf("  %-15s %12d (%.2fM)\n", "TOTAL", sum, float64(sum)/1_000_000)
}

func randomTime(maxDays int) time.Time {
	return time.Now().Add(-time.Duration(rand.Intn(maxDays)) * 24 * time.Hour)
}

func getEnvKey(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
