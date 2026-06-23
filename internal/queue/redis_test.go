package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

func TestRedisQueue_EnqueueDequeue(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	q, err := NewRedisQueue(client, zap.NewNop())
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}

	event := &models.Event{
		ID:        uuid.New(),
		Type:      "queue.test",
		Source:    "unit",
		Payload:   json.RawMessage(`{"k":"v"}`),
		CreatedAt: time.Now(),
	}

	ctx := context.Background()
	if err := q.EnqueueEvent(ctx, event); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, err := q.DequeueEvent(ctx, time.Second)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got == nil {
		t.Fatal("expected event")
	}
	if got.ID != event.ID {
		t.Fatalf("id mismatch: %s vs %s", got.ID, event.ID)
	}
	if got.Type != event.Type {
		t.Fatalf("type mismatch")
	}
}

func TestRedisQueue_DequeueTimeout(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	q, _ := NewRedisQueue(client, zap.NewNop())

	got, err := q.DequeueEvent(context.Background(), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil on timeout")
	}
}

func TestRedisQueue_FIFOOrder(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	q, _ := NewRedisQueue(client, zap.NewNop())
	ctx := context.Background()

	first := &models.Event{ID: uuid.New(), Type: "first", Source: "s", CreatedAt: time.Now()}
	second := &models.Event{ID: uuid.New(), Type: "second", Source: "s", CreatedAt: time.Now()}

	_ = q.EnqueueEvent(ctx, first)
	_ = q.EnqueueEvent(ctx, second)

	got, err := q.DequeueEvent(ctx, time.Second)
	if err != nil || got.Type != "first" {
		t.Fatalf("expected FIFO order (first out first), got %#v err=%v", got, err)
	}
}
