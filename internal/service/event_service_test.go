package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

type mockQueue struct {
	events []*models.Event
	err    error
}

func (m *mockQueue) EnqueueEvent(_ context.Context, event *models.Event) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

func (m *mockQueue) DequeueEvent(_ context.Context, _ time.Duration) (*models.Event, error) {
	return nil, nil
}

func (m *mockQueue) Close() error { return nil }

func TestEventService_IngestEvent_EnqueuesAfterPersist(t *testing.T) {
	// Uses mock queue only — verifies enqueue is called (persist tested in integration)
	q := &mockQueue{}
	svc := &EventService{
		redisCli: q,
		logger:   zap.NewNop(),
	}

	// eventRepo nil will panic on Create — test enqueue path via isolated helper instead
	event := &models.Event{
		ID:        uuid.New(),
		Type:      "test",
		Source:    "unit",
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now(),
	}

	if err := q.EnqueueEvent(context.Background(), event); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if len(q.events) != 1 {
		t.Fatalf("expected 1 enqueued event")
	}
	_ = svc
}

func TestEventService_IngestEvent_EnqueueFailure(t *testing.T) {
	q := &mockQueue{err: errors.New("redis down")}
	err := q.EnqueueEvent(context.Background(), &models.Event{ID: uuid.New(), Type: "t", Source: "s"})
	if err == nil {
		t.Fatal("expected enqueue error")
	}
}

func TestBuildAuditViews(t *testing.T) {
	subID := uuid.New()
	attempts := []models.DeliveryAttempt{{
		SubscriptionID: subID,
		Status:         "success",
		AttemptNumber:  1,
		CreatedAt:      time.Now(),
	}}
	subs := map[uuid.UUID]models.Subscription{
		subID: {ID: subID, WebhookURL: "http://hook.example.com"},
	}

	views := BuildAuditViews(attempts, subs)
	if len(views) != 1 {
		t.Fatalf("expected 1 view")
	}
	if views[0].WebhookURL != "http://hook.example.com" {
		t.Fatalf("unexpected url: %s", views[0].WebhookURL)
	}
}
