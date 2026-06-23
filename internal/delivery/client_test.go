package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

func TestClient_DeliverSuccess(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(5, 1024)
	event := &models.Event{
		ID:        uuid.New(),
		Type:      "test.event",
		Source:    "tests",
		Payload:   json.RawMessage(`{"hello":"world"}`),
		CreatedAt: time.Now(),
	}

	result := client.Deliver(context.Background(), server.URL, event)
	if result.Err != nil {
		t.Fatalf("delivery failed: %v", result.Err)
	}
	if !IsSuccess(result.HTTPCode) {
		t.Fatalf("expected success status, got %d", result.HTTPCode)
	}
	if received["type"] != "test.event" {
		t.Fatalf("unexpected payload: %#v", received)
	}
}

func TestClient_DeliverServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(5, 1024)
	event := &models.Event{ID: uuid.New(), Type: "test", Source: "tests", CreatedAt: time.Now()}
	result := client.Deliver(context.Background(), server.URL, event)
	if result.Err != nil {
		t.Fatalf("unexpected transport error: %v", result.Err)
	}
	if IsSuccess(result.HTTPCode) {
		t.Fatal("expected non-success status")
	}
}

func TestIsClientError(t *testing.T) {
	if !IsClientError(404) {
		t.Fatal("expected 404 to be client error")
	}
	if IsClientError(503) {
		t.Fatal("expected 503 not to be client error")
	}
}
