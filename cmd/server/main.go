package main

import (
	"context"
	"fmt"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/config"
	httphandler "github.com/event-fanout-service/event-fanout/internal/http"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/repository"
	"github.com/event-fanout-service/event-fanout/internal/service"
)

func main() {
	// Load configuration
	cfg := config.NewConfig()

	// Initialize logger
	logger := initLogger(cfg.LogLevel)
	defer logger.Sync()

	logger.Info("Starting event fanout service", zap.String("version", "0.1.0"), zap.String("env", cfg.Env))

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
	logger.Info("Connecting to Redis", zap.String("redis_url", cfg.RedisURL))
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379", // In production, parse from cfg.RedisURL
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}
	logger.Info("Successfully connected to Redis")

	// Initialize repositories
	eventRepo := repository.NewEventRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	deliveryRepo := repository.NewDeliveryRepository(db)

	// Initialize queue
	redisCli, err := queue.NewRedisQueue(redisClient, logger)
	if err != nil {
		logger.Fatal("failed to create Redis queue", zap.Error(err))
	}
	if err := redisCli.InitConsumerGroup(ctx); err != nil {
		logger.Warn("failed to initialize consumer group", zap.Error(err))
	}

	// Initialize services
	eventService := service.NewEventService(eventRepo, subRepo, deliveryRepo, redisCli, logger)
	subscriptionService := service.NewSubscriptionService(subRepo, logger)

	// Initialize HTTP handlers
	handler := httphandler.NewHandler(eventService, subscriptionService, logger)

	// Create router
	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	server := &nethttp.Server{
		Addr:    addr,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("Starting HTTP server", zap.String("addr", addr))
		if err := server.ListenAndServe(); err != nil && err != nethttp.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	<-sigChan

	logger.Info("Received shutdown signal, gracefully shutting down...")

	// Shutdown server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("Server stopped")
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
