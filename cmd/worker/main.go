package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/config"
	"github.com/event-fanout-service/event-fanout/internal/delivery"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/redisutil"
	"github.com/event-fanout-service/event-fanout/internal/repository"
	"github.com/event-fanout-service/event-fanout/internal/service"
	"github.com/event-fanout-service/event-fanout/internal/worker"
)

func main() {
	cfg := config.NewConfig()

	logger := initLogger(cfg.LogLevel)
	defer logger.Sync()

	logger.Info("Starting event fanout worker", zap.String("version", "0.1.0"), zap.String("env", cfg.Env))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := repository.NewDBPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("failed to connect to PostgreSQL", zap.Error(err))
	}
	defer db.Close()

	redisClient, err := redisutil.NewClient(cfg.RedisURL)
	if err != nil {
		logger.Fatal("failed to parse Redis URL", zap.Error(err))
	}
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}

	eventRepo := repository.NewEventRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	deliveryRepo := repository.NewDeliveryRepository(db)

	redisQueue, err := queue.NewRedisQueue(redisClient, logger, cfg.QueueConfig())
	if err != nil {
		logger.Fatal("failed to create Redis queue", zap.Error(err))
	}

	webhookClient := delivery.NewClient(cfg.WebhookTimeoutSeconds, cfg.WebhookMaxBodyBytes)
	fanoutService := service.NewFanoutService(
		eventRepo,
		subRepo,
		deliveryRepo,
		webhookClient,
		cfg.MaxDeliveryRetries,
		cfg.BaseRetryDelaySeconds,
		logger,
	)

	processor := worker.NewProcessor(
		redisQueue,
		fanoutService,
		5*time.Second,
		2*time.Second,
		cfg.FanoutWorkerPoolSize,
		logger,
	)

	go processor.Run(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	<-sigChan

	cancel()
	logger.Info("Worker stopped")
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
