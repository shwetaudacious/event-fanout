//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/delivery"
	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/redisutil"
	"github.com/event-fanout-service/event-fanout/internal/repository"
	"github.com/event-fanout-service/event-fanout/internal/service"
	"github.com/event-fanout-service/event-fanout/internal/worker"
)

type testEnv struct {
	ctx          context.Context
	cancel       context.CancelFunc
	pool         *pgxpool.Pool
	eventRepo    *repository.EventRepository
	subRepo      *repository.SubscriptionRepository
	deliveryRepo *repository.DeliveryRepository
	eventService *service.EventService
	subService   *service.SubscriptionService
	queue        *queue.RedisQueue
	redis        *redis.Client
	logger       *zap.Logger
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()

	databaseURL := envOrDefault("DATABASE_URL", "postgres://postgres:postgres123@localhost:5432/eventfanout?sslmode=disable")
	redisURL := envOrDefault("REDIS_URL", "redis://localhost:6379")

	ctx, cancel := context.WithCancel(context.Background())
	logger := zap.NewNop()

	pool, err := repository.NewDBPool(ctx, databaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		pool.Close()
	})

	migrationSQL, err := os.ReadFile("../../migrations/001_init_schema.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if err := repository.RunMigrations(ctx, pool, string(migrationSQL)); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	redisClient, err := redisutil.NewClient(redisURL)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() { redisClient.Close() })
	_ = redisClient.FlushDB(ctx)

	eventRepo := repository.NewEventRepository(pool)
	subRepo := repository.NewSubscriptionRepository(pool)
	deliveryRepo := repository.NewDeliveryRepository(pool)
	redisQueue, err := queue.NewRedisQueue(redisClient, logger)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}

	return &testEnv{
		ctx:          ctx,
		cancel:       cancel,
		pool:         pool,
		eventRepo:    eventRepo,
		subRepo:      subRepo,
		deliveryRepo: deliveryRepo,
		eventService: service.NewEventService(eventRepo, subRepo, deliveryRepo, redisQueue, logger),
		subService:   service.NewSubscriptionService(subRepo, logger),
		queue:        redisQueue,
		redis:        redisClient,
		logger:       logger,
	}
}

func (e *testEnv) startWorker(t *testing.T, maxRetries, baseDelaySec int) {
	t.Helper()
	webhookClient := delivery.NewClient(5, 1024)
	fanout := service.NewFanoutService(
		e.eventRepo, e.subRepo, e.deliveryRepo,
		webhookClient, maxRetries, baseDelaySec, e.logger,
	)
	processor := worker.NewProcessor(e.queue, fanout, 500*time.Millisecond, 500*time.Millisecond, 10, e.logger)
	go processor.Run(e.ctx)
}

func waitForAuditStatus(t *testing.T, svc *service.EventService, eventID uuid.UUID, wantStatus string) *models.DeliveryAudit {
	t.Helper()
	var audit *models.DeliveryAudit
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		audit, err = svc.GetEventAudit(context.Background(), eventID, 10, 0)
		if err == nil && len(audit.Attempts) > 0 && audit.Attempts[0].Status == wantStatus {
			return audit
		}
		time.Sleep(100 * time.Millisecond)
	}
	if audit != nil && len(audit.Attempts) > 0 {
		t.Fatalf("timed out waiting for status %q, got %q", wantStatus, audit.Attempts[0].Status)
	}
	t.Fatalf("timed out waiting for audit status %q", wantStatus)
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
