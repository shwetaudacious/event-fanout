//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/delivery"
	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/redisutil"
	"github.com/event-fanout-service/event-fanout/internal/repository"
	"github.com/event-fanout-service/event-fanout/internal/service"
	"github.com/event-fanout-service/event-fanout/internal/worker"
)

func TestEndToEndFanout(t *testing.T) {
	databaseURL := envOrDefault("DATABASE_URL", "postgres://postgres:postgres123@localhost:5432/eventfanout?sslmode=disable")
	redisURL := envOrDefault("REDIS_URL", "redis://localhost:6379")

	ctx := context.Background()
	logger := zap.NewNop()

	db, err := repository.NewDBPool(ctx, databaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer db.Close()

	migrationSQL, err := os.ReadFile("../../migrations/001_init_schema.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if err := repository.RunMigrations(ctx, db, string(migrationSQL)); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	redisClient, err := redisutil.NewClient(redisURL)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	defer redisClient.Close()
	_ = redisClient.FlushDB(ctx)

	delivered := make(chan string, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered <- r.Header.Get("X-Event-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	eventRepo := repository.NewEventRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	deliveryRepo := repository.NewDeliveryRepository(db)
	redisQueue, err := queue.NewRedisQueue(redisClient, logger)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}

	eventService := service.NewEventService(eventRepo, subRepo, deliveryRepo, redisQueue, logger)

	subService := service.NewSubscriptionService(subRepo, logger)
	sub, err := subService.CreateSubscription(ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules:      json.RawMessage(`{"type":"integration.test","source":"ci"}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	webhookClient := delivery.NewClient(5, 1024)
	fanoutService := service.NewFanoutService(eventRepo, subRepo, deliveryRepo, webhookClient, 3, 1, logger)
	processor := worker.NewProcessor(redisQueue, fanoutService, time.Second, time.Second, 10, logger)

	procCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go processor.Run(procCtx)

	event, err := eventService.IngestEvent(ctx, &models.CreateEventRequest{
		Type:    "integration.test",
		Source:  "ci",
		Payload: json.RawMessage(`{"case":"fanout"}`),
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	select {
	case got := <-delivered:
		if got != event.ID.String() {
			t.Fatalf("expected event id %s, got %s", event.ID, got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}

	var audit *models.DeliveryAudit
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		audit, err = eventService.GetEventAudit(ctx, event.ID, 10, 0)
		if err != nil {
			t.Fatalf("get audit: %v", err)
		}
		if len(audit.Attempts) > 0 && audit.Attempts[0].Status == "success" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if audit == nil || len(audit.Attempts) == 0 {
		t.Fatal("expected audit attempts")
	}
	if audit.Attempts[0].Status != "success" {
		t.Fatalf("expected success status, got %s", audit.Attempts[0].Status)
	}
	if audit.Attempts[0].WebhookURL != sub.WebhookURL {
		t.Fatalf("unexpected webhook url: %s", audit.Attempts[0].WebhookURL)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
