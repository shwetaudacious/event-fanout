package config

import (
	"os"
	"strconv"

	"github.com/event-fanout-service/event-fanout/internal/queue"
)

// Config holds application configuration.
type Config struct {
	ServerPort              int
	ServerHost              string
	DatabaseURL             string
	RedisURL                string
	RedisStreamKey          string
	RedisConsumerGroup      string
	RedisConsumerName       string
	MaxWorkers              int
	EventProcessorWorkers   int
	FanoutWorkerPoolSize    int
	MaxDeliveryRetries      int
	BaseRetryDelaySeconds   int
	WebhookTimeoutSeconds   int
	WebhookMaxBodyBytes     int64
	LogLevel                string
	Env                     string
}

// NewConfig loads configuration from environment variables.
func NewConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "worker"
	}

	return &Config{
		ServerPort:            getEnvInt("SERVER_PORT", 8080),
		ServerHost:            getEnv("SERVER_HOST", "0.0.0.0"),
		DatabaseURL:           getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/eventfanout"),
		RedisURL:              getEnv("REDIS_URL", "redis://localhost:6379"),
		RedisStreamKey:        getEnv("REDIS_STREAM_KEY", "events:stream"),
		RedisConsumerGroup:    getEnv("REDIS_CONSUMER_GROUP", "fanout-workers"),
		RedisConsumerName:     getEnv("REDIS_CONSUMER_NAME", hostname),
		MaxWorkers:            getEnvInt("MAX_WORKERS", 5),
		EventProcessorWorkers: getEnvInt("EVENT_PROCESSOR_WORKERS", 2),
		FanoutWorkerPoolSize:  getEnvInt("FANOUT_WORKER_POOL", 10),
		MaxDeliveryRetries:    getEnvInt("MAX_DELIVERY_RETRIES", 5),
		BaseRetryDelaySeconds: getEnvInt("BASE_RETRY_DELAY_SECONDS", 5),
		WebhookTimeoutSeconds: getEnvInt("WEBHOOK_TIMEOUT_SECONDS", 30),
		WebhookMaxBodyBytes:   int64(getEnvInt("WEBHOOK_MAX_BODY_BYTES", 1048576)),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		Env:                   getEnv("ENVIRONMENT", "development"),
	}
}

// QueueConfig returns Redis Streams settings for the queue layer.
func (c *Config) QueueConfig() queue.Config {
	return queue.Config{
		StreamKey:     c.RedisStreamKey,
		ConsumerGroup: c.RedisConsumerGroup,
		ConsumerName:  c.RedisConsumerName,
	}
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultVal
}
