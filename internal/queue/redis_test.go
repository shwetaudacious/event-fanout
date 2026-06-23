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

func testQueue(t *testing.T) (*RedisQueue, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	q, err := NewRedisQueue(client, zap.NewNop(), Config{
		StreamKey:     "test:stream",
		ConsumerGroup: "test-group",
		ConsumerName:  "test-consumer",
	})
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	return q, mr
}

func TestRedisStream_EnqueueReadAck(t *testing.T) {
	q, mr := testQueue(t)
	defer mr.Close()

	ctx := context.Background()
	if err := q.InitConsumerGroup(ctx); err != nil {
		t.Fatalf("init group: %v", err)
	}

	event := &models.Event{
		ID:        uuid.New(),
		Type:      "queue.test",
		Source:    "unit",
		Payload:   json.RawMessage(`{"k":"v"}`),
		CreatedAt: time.Now(),
	}
	if err := q.EnqueueEvent(ctx, event); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	msg, err := q.ReadEvent(ctx, time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg == nil || msg.Event.ID != event.ID {
		t.Fatalf("unexpected message: %#v", msg)
	}

	if err := q.AckEvent(ctx, msg.StreamID); err != nil {
		t.Fatalf("ack: %v", err)
	}
}

func TestRedisStream_ReadTimeout(t *testing.T) {
	q, mr := testQueue(t)
	defer mr.Close()

	ctx := context.Background()
	if err := q.InitConsumerGroup(ctx); err != nil {
		t.Fatalf("init group: %v", err)
	}

	msg, err := q.ReadEvent(ctx, time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg != nil {
		t.Fatal("expected nil on timeout")
	}
}

func TestRedisStream_FIFOOrder(t *testing.T) {
	q, mr := testQueue(t)
	defer mr.Close()

	ctx := context.Background()
	if err := q.InitConsumerGroup(ctx); err != nil {
		t.Fatalf("init group: %v", err)
	}

	first := &models.Event{ID: uuid.New(), Type: "first", Source: "s", CreatedAt: time.Now()}
	second := &models.Event{ID: uuid.New(), Type: "second", Source: "s", CreatedAt: time.Now()}

	_ = q.EnqueueEvent(ctx, first)
	_ = q.EnqueueEvent(ctx, second)

	msg, err := q.ReadEvent(ctx, time.Second)
	if err != nil || msg.Event.Type != "first" {
		t.Fatalf("expected first event, got %#v err=%v", msg, err)
	}
	_ = q.AckEvent(ctx, msg.StreamID)

	msg, err = q.ReadEvent(ctx, time.Second)
	if err != nil || msg.Event.Type != "second" {
		t.Fatalf("expected second event, got %#v err=%v", msg, err)
	}
}

func TestRedisStream_InitConsumerGroupIdempotent(t *testing.T) {
	q, mr := testQueue(t)
	defer mr.Close()

	ctx := context.Background()
	if err := q.InitConsumerGroup(ctx); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := q.InitConsumerGroup(ctx); err != nil {
		t.Fatalf("second init should be idempotent: %v", err)
	}
}
