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
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/config"
	httphandler "github.com/event-fanout-service/event-fanout/internal/http"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/redisutil"
	"github.com/event-fanout-service/event-fanout/internal/repository"
	"github.com/event-fanout-service/event-fanout/internal/service"
)

func main() {
	cfg := config.NewConfig()

	logger := initLogger(cfg.LogLevel)
	defer logger.Sync()

	logger.Info("Starting event fanout service", zap.String("version", "0.1.0"), zap.String("env", cfg.Env))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := repository.NewDBPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("failed to connect to PostgreSQL", zap.Error(err))
	}
	defer db.Close()
	logger.Info("Successfully connected to PostgreSQL")

	redisClient, err := redisutil.NewClient(cfg.RedisURL)
	if err != nil {
		logger.Fatal("failed to parse Redis URL", zap.Error(err))
	}
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}
	logger.Info("Successfully connected to Redis")

	eventRepo := repository.NewEventRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	deliveryRepo := repository.NewDeliveryRepository(db)

	redisQueue, err := queue.NewRedisQueue(redisClient, logger, cfg.QueueConfig())
	if err != nil {
		logger.Fatal("failed to create Redis queue", zap.Error(err))
	}
	if err := redisQueue.InitConsumerGroup(ctx); err != nil {
		logger.Warn("failed to initialize stream consumer group", zap.Error(err))
	}

	eventService := service.NewEventService(eventRepo, subRepo, deliveryRepo, redisQueue, logger)
	subscriptionService := service.NewSubscriptionService(subRepo, logger)

	handler := httphandler.NewHandler(eventService, subscriptionService, logger, db, redisClient)

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	server := &nethttp.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		logger.Info("Starting HTTP server", zap.String("addr", addr))
		if err := server.ListenAndServe(); err != nil && err != nethttp.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	<-sigChan

	logger.Info("Received shutdown signal, gracefully shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("Server stopped")
}

func initLogger(level string) *zap.Logger {
	var cfg zap.Config
	switch level {
	case "debug":
		cfg = zap.NewDevelopmentConfig()
	default:
		cfg = zap.NewProductionConfig()
	}

	logger, _ := cfg.Build()
	return logger
}
