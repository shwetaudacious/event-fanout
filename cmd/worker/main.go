package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/config"
	"github.com/event-fanout-service/event-fanout/internal/repository"
)

func main() {
	// Load configuration
	cfg := config.NewConfig()

	// Initialize logger
	logger := initLogger(cfg.LogLevel)
	defer logger.Sync()

	logger.Info("Starting event fanout worker", zap.String("version", "0.1.0"), zap.String("env", cfg.Env))

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize database connection pool
	logger.Info("Connecting to PostgreSQL", zap.String("database_url", cfg.DatabaseURL))
	db, err := repository.NewDBPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("failed to connect to PostgreSQL", zap.Error(err))
	}
	defer db.Close()
	logger.Info("Successfully connected to PostgreSQL")

	// Initialize Redis connection
	logger.Info("Connecting to Redis")
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}
	logger.Info("Successfully connected to Redis")

	// TODO: Implement worker to process events from queue
	// This will involve:
	// 1. Creating repositories (event, subscription, delivery)
	// 2. Consuming events from Redis queue
	// 3. Matching events against subscriptions
	// 4. Creating delivery attempts for matched subscriptions
	// 5. Sending webhooks with retry logic
	logger.Info("Worker initialized and ready")
	logger.Info("Event processing not yet implemented - worker will exit shortly for now")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	<-sigChan

	logger.Info("Received shutdown signal, shutting down...")
	logger.Info("Worker stopped")
}

// initLogger initializes the zap logger
func initLogger(level string) *zap.Logger {
	var cfg zap.Config
	switch level {
	case "debug":
		cfg = zap.NewDevelopmentConfig()
	case "info", "warn", "error":
		cfg = zap.NewProductionConfig()
	default:
		cfg = zap.NewProductionConfig()
	}

	logger, _ := cfg.Build()
	return logger
}
