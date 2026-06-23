package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

const eventsQueueKey = "events:queue"

// Queue defines queue operations.
type Queue interface {
	EnqueueEvent(ctx context.Context, event *models.Event) error
	DequeueEvent(ctx context.Context, timeout time.Duration) (*models.Event, error)
	Close() error
}

// RedisQueue implements Queue using a Redis list.
type RedisQueue struct {
	client *redis.Client
	logger *zap.Logger
}

// NewRedisQueue creates a new Redis queue.
func NewRedisQueue(client *redis.Client, logger *zap.Logger) (*RedisQueue, error) {
	return &RedisQueue{client: client, logger: logger}, nil
}

// EnqueueEvent adds an event to the queue.
func (q *RedisQueue) EnqueueEvent(ctx context.Context, event *models.Event) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return q.client.LPush(ctx, eventsQueueKey, string(eventJSON)).Err()
}

// DequeueEvent blocks until an event is available or timeout elapses.
func (q *RedisQueue) DequeueEvent(ctx context.Context, timeout time.Duration) (*models.Event, error) {
	result, err := q.client.BRPop(ctx, timeout, eventsQueueKey).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(result) < 2 {
		return nil, nil
	}

	var event models.Event
	if err := json.Unmarshal([]byte(result[1]), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// InitConsumerGroup is retained for future Redis Streams migration.
func (q *RedisQueue) InitConsumerGroup(ctx context.Context) error {
	return nil
}

// Close closes the queue connection.
func (q *RedisQueue) Close() error {
	return q.client.Close()
}
