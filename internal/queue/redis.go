package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

const (
	defaultStreamKey     = "events:stream"
	defaultConsumerGroup = "fanout-workers"
)

// Config holds Redis Streams settings.
type Config struct {
	StreamKey     string
	ConsumerGroup string
	ConsumerName  string
}

// DefaultConfig returns production-oriented stream defaults.
func DefaultConfig() Config {
	name, _ := os.Hostname()
	if name == "" {
		name = "worker"
	}
	return Config{
		StreamKey:     defaultStreamKey,
		ConsumerGroup: defaultConsumerGroup,
		ConsumerName:  name,
	}
}

// EventMessage is a stream entry with its Redis ID for acknowledgment.
type EventMessage struct {
	StreamID string
	Event    *models.Event
}

// Queue defines fanout queue operations backed by Redis Streams.
type Queue interface {
	EnqueueEvent(ctx context.Context, event *models.Event) error
	InitConsumerGroup(ctx context.Context) error
	ReadEvent(ctx context.Context, timeout time.Duration) (*EventMessage, error)
	AckEvent(ctx context.Context, streamID string) error
	ReclaimPending(ctx context.Context, minIdle time.Duration, count int64) ([]EventMessage, error)
	Close() error
}

// RedisQueue implements Queue using Redis Streams and consumer groups.
type RedisQueue struct {
	client *redis.Client
	logger *zap.Logger
	cfg    Config
}

// NewRedisQueue creates a Redis Streams queue.
func NewRedisQueue(client *redis.Client, logger *zap.Logger, cfg Config) (*RedisQueue, error) {
	if cfg.StreamKey == "" {
		cfg.StreamKey = defaultStreamKey
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = defaultConsumerGroup
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = DefaultConfig().ConsumerName
	}
	return &RedisQueue{client: client, logger: logger, cfg: cfg}, nil
}

// EnqueueEvent appends an event to the stream (XADD).
func (q *RedisQueue) EnqueueEvent(ctx context.Context, event *models.Event) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.cfg.StreamKey,
		Values: map[string]interface{}{"event": string(eventJSON)},
	}).Err()
}

// InitConsumerGroup creates the stream consumer group if missing.
func (q *RedisQueue) InitConsumerGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.cfg.StreamKey, q.cfg.ConsumerGroup, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create consumer group: %w", err)
	}
	return nil
}

// ReadEvent reads the next event from the consumer group (XREADGROUP).
func (q *RedisQueue) ReadEvent(ctx context.Context, timeout time.Duration) (*EventMessage, error) {
	if timeout < time.Second {
		timeout = time.Second
	}

	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.cfg.ConsumerGroup,
		Consumer: q.cfg.ConsumerName,
		Streams:  []string{q.cfg.StreamKey, ">"},
		Count:    1,
		Block:    timeout,
	}).Result()

	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	msg, ok := firstStreamMessage(streams)
	if !ok {
		return nil, nil
	}
	return parseStreamMessage(msg)
}

// AckEvent acknowledges successful processing (XACK).
func (q *RedisQueue) AckEvent(ctx context.Context, streamID string) error {
	return q.client.XAck(ctx, q.cfg.StreamKey, q.cfg.ConsumerGroup, streamID).Err()
}

// ReclaimPending reassigns stale pending messages via XAUTOCLAIM.
func (q *RedisQueue) ReclaimPending(ctx context.Context, minIdle time.Duration, count int64) ([]EventMessage, error) {
	if count <= 0 {
		count = 10
	}

	result, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   q.cfg.StreamKey,
		Group:    q.cfg.ConsumerGroup,
		Consumer: q.cfg.ConsumerName,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    count,
	}).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	messages := make([]EventMessage, 0, len(result))
	for _, msg := range result {
		parsed, err := parseStreamMessage(msg)
		if err != nil {
			q.logger.Warn("skip malformed stream message", zap.String("id", msg.ID), zap.Error(err))
			continue
		}
		messages = append(messages, *parsed)
	}
	return messages, nil
}

// Close closes the underlying Redis client.
func (q *RedisQueue) Close() error {
	return q.client.Close()
}

func firstStreamMessage(streams []redis.XStream) (redis.XMessage, bool) {
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return redis.XMessage{}, false
	}
	return streams[0].Messages[0], true
}

func parseStreamMessage(msg redis.XMessage) (*EventMessage, error) {
	raw, ok := msg.Values["event"].(string)
	if !ok {
		return nil, errors.New("missing event field in stream message")
	}
	var event models.Event
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return nil, fmt.Errorf("unmarshal event: %w", err)
	}
	return &EventMessage{StreamID: msg.ID, Event: &event}, nil
}

func isBusyGroup(err error) bool {
	return err != nil && err.Error() == "BUSYGROUP Consumer Group name already exists"
}
