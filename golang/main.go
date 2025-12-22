package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Database connection - support both DATABASE_URL and individual vars
	dbURL := getEnv("DATABASE_URL", "")
	if dbURL == "" {
		dbHost := getEnv("DB_HOST", "localhost")
		dbPort := getEnv("DB_PORT", "5434")
		dbName := getEnv("DB_NAME", "loadtest")
		dbUser := getEnv("DB_USER", "postgres")
		dbPassword := getEnv("DB_PASSWORD", "postgres")
		dbURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
			dbUser, dbPassword, dbHost, dbPort, dbName)
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}
	log.Println("✅ PostgreSQL connected")

	// Redis connection - support both REDIS_URL and individual vars
	redisAddr := getEnv("REDIS_URL", "")
	if redisAddr == "" {
		redisHost := getEnv("REDIS_HOST", "localhost")
		redisPort := getEnv("REDIS_PORT", "6381")
		redisAddr = fmt.Sprintf("%s:%s", redisHost, redisPort)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		PoolSize: 20,
	})
	defer rdb.Close()

	// Test Redis connection
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Unable to connect to Redis: %v", err)
	}
	log.Println("✅ Redis connected")

	// Initialize handlers
	userHandler := NewUserOverviewHandler(pool, rdb)
	checkoutHandler := NewCheckoutHandler(pool, rdb)

	// Create Fiber app with optimized config
	app := fiber.New(fiber.Config{
		Prefork:               false,
		CaseSensitive:         true,
		StrictRouting:         true,
		ServerHeader:          "Fiber",
		AppName:               "LoadTest Benchmark",
		DisableStartupMessage: false,
	})

	// Middleware
	app.Use(recover.New())

	// Routes
	v1 := app.Group("/v1")
	v1.Get("/users/:userId/overview", userHandler.GetUserOverview)
	v1.Post("/checkout", checkoutHandler.Checkout)

	// Health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	port := getEnv("PORT", "3001")
	log.Printf("🚀 Fiber server running on port %s", port)
	log.Fatal(app.Listen(":" + port))
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
