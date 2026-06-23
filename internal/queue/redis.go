package queue

import (
"context"
"encoding/json"

"github.com/redis/go-redis/v9"
"go.uber.org/zap"

"github.com/event-fanout-service/event-fanout/internal/models"
)

// Queue defines queue operations
type Queue interface {
EnqueueEvent(ctx context.Context, event *models.Event) error
Close() error
}

// RedisQueue implements Queue using Redis
type RedisQueue struct {
client *redis.Client
logger *zap.Logger
}

// NewRedisQueue creates a new Redis queue
func NewRedisQueue(client *redis.Client, logger *zap.Logger) (*RedisQueue, error) {
rq := &RedisQueue{
client: client,
logger: logger,
}
return rq, nil
}

// EnqueueEvent adds an event to the queue
func (q *RedisQueue) EnqueueEvent(ctx context.Context, event *models.Event) error {
eventJSON, _ := json.Marshal(event)
return q.client.LPush(ctx, "events:queue", string(eventJSON)).Err()
}

// InitConsumerGroup initializes consumer group
func (q *RedisQueue) InitConsumerGroup(ctx context.Context) error {
return nil
}

// Close closes the queue connection
func (q *RedisQueue) Close() error {
return q.client.Close()
}
