package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event represents an ingested event
type Event struct {
	ID        uuid.UUID           `json:"id" db:"id"`
	Type      string              `json:"type" db:"type"`
	Source    string              `json:"source" db:"source"`
	Payload   json.RawMessage     `json:"payload" db:"payload"`
	CreatedAt time.Time           `json:"created_at" db:"created_at"`
}

// CreateEventRequest is the payload for creating an event
type CreateEventRequest struct {
	Type    string          `json:"type" validate:"required"`
	Source  string          `json:"source" validate:"required"`
	Payload json.RawMessage `json:"payload"`
}

// Subscription represents a webhook subscription with filter rules
type Subscription struct {
	ID        uuid.UUID       `json:"id" db:"id"`
	WebhookURL string          `json:"webhook_url" db:"webhook_url"`
	Rules     json.RawMessage `json:"rules" db:"rules"`
	Active    bool            `json:"active" db:"active"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// CreateSubscriptionRequest is the payload for creating a subscription
type CreateSubscriptionRequest struct {
	WebhookURL string          `json:"webhook_url" validate:"required,url"`
	Rules      json.RawMessage `json:"rules" validate:"required"`
}

// SubscriptionRules defines filter criteria for matching events
type SubscriptionRules struct {
	Type          string         `json:"type,omitempty"`
	Source        string         `json:"source,omitempty"`
	PayloadRules  []PayloadRule  `json:"payload_rules,omitempty"`
}

// PayloadRule defines a single JSON path-based condition
type PayloadRule struct {
	Path  string      `json:"path"`  // JSON path (e.g., "$.user.role")
	Op    string      `json:"op"`    // "==", "!=", "in", "regex"
	Value interface{} `json:"value"`
}

// DeliveryAttempt tracks webhook delivery attempts for an event to a subscription
type DeliveryAttempt struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	EventID         uuid.UUID  `json:"event_id" db:"event_id"`
	SubscriptionID  uuid.UUID  `json:"subscription_id" db:"subscription_id"`
	AttemptNumber   int        `json:"attempt_number" db:"attempt_number"`
	Status          string     `json:"status" db:"status"` // pending, success, failed
	HTTPCode        *int       `json:"http_code,omitempty" db:"http_code"`
	ErrorMessage    *string    `json:"error_message,omitempty" db:"error_message"`
	NextRetryAt     *time.Time `json:"next_retry_at,omitempty" db:"next_retry_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
}

// DeliveryAudit is the response for delivery history queries
type DeliveryAudit struct {
	EventID       uuid.UUID              `json:"event_id"`
	Event         *Event                 `json:"event,omitempty"`
	Subscriptions int                    `json:"total_subscriptions"`
	Attempts      []DeliveryAttemptView  `json:"attempts"`
}

// DeliveryAttemptView is a view of delivery attempts for audit
type DeliveryAttemptView struct {
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	WebhookURL     string     `json:"webhook_url"`
	Status         string     `json:"status"`
	AttemptNumber  int        `json:"attempt_number"`
	HTTPCode       *int       `json:"http_code,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	Timestamp      time.Time  `json:"timestamp"`
}

// PaginationQuery holds pagination params
type PaginationQuery struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Cursor string `json:"cursor,omitempty"`
}

// HealthCheckResponse is the response for health checks
type HealthCheckResponse struct {
	Status   string `json:"status"`
	Database bool   `json:"database"`
	Redis    bool   `json:"redis"`
	Message  string `json:"message"`
}
