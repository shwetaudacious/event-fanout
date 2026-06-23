package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

// Result captures the outcome of a webhook delivery attempt.
type Result struct {
	HTTPCode int
	Body     string
	Err      error
}

// Client posts events to subscriber webhook URLs.
type Client struct {
	httpClient     *http.Client
	maxBodyBytes   int64
}

// NewClient creates a webhook delivery client.
func NewClient(timeoutSeconds int, maxBodyBytes int64) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
		maxBodyBytes: maxBodyBytes,
	}
}

// Deliver posts the event payload to the webhook URL.
func (c *Client) Deliver(ctx context.Context, webhookURL string, event *models.Event) Result {
	payload, err := json.Marshal(map[string]interface{}{
		"id":         event.ID,
		"type":       event.Type,
		"source":     event.Source,
		"payload":    json.RawMessage(event.Payload),
		"created_at": event.CreatedAt,
	})
	if err != nil {
		return Result{Err: fmt.Errorf("marshal payload: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return Result{Err: fmt.Errorf("create request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Id", event.ID.String())
	req.Header.Set("X-Event-Type", event.Type)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{Err: fmt.Errorf("post webhook: %w", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, c.maxBodyBytes))
	return Result{
		HTTPCode: resp.StatusCode,
		Body:     string(body),
	}
}

// IsSuccess returns true for 2xx responses.
func IsSuccess(code int) bool {
	return code >= 200 && code < 300
}

// IsClientError returns true for 4xx responses that should not be retried.
func IsClientError(code int) bool {
	return code >= 400 && code < 500
}
