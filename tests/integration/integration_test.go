//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

func TestEndToEndFanout(t *testing.T) {
	env := setupEnv(t)
	env.startWorker(t, 3, 1)

	delivered := make(chan string, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered <- r.Header.Get("X-Event-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	sub, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules:      json.RawMessage(`{"type":"integration.test","source":"ci"}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	event, err := env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:    "integration.test",
		Source:  "ci",
		Payload: json.RawMessage(`{"case":"fanout"}`),
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	select {
	case got := <-delivered:
		if got != event.ID.String() {
			t.Fatalf("expected event id %s, got %s", event.ID, got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}

	audit := waitForAuditStatus(t, env.eventService, event.ID, "success")
	if audit.Attempts[0].WebhookURL != sub.WebhookURL {
		t.Fatalf("unexpected webhook url: %s", audit.Attempts[0].WebhookURL)
	}
	if audit.Attempts[0].HTTPCode == nil || *audit.Attempts[0].HTTPCode != 200 {
		t.Fatalf("expected http 200 in audit")
	}
}

func TestNoMatchingSubscription(t *testing.T) {
	env := setupEnv(t)
	env.startWorker(t, 3, 1)

	called := make(chan struct{}, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	_, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules:      json.RawMessage(`{"type":"other.event"}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	event, err := env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:   "integration.test",
		Source: "ci",
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	time.Sleep(2 * time.Second)

	select {
	case <-called:
		t.Fatal("webhook should not be called for non-matching subscription")
	default:
	}

	audit, err := env.eventService.GetEventAudit(env.ctx, event.ID, 10, 0)
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if len(audit.Attempts) != 0 {
		t.Fatalf("expected no delivery attempts, got %d", len(audit.Attempts))
	}
}

func TestRetryOn5xxThenSuccess(t *testing.T) {
	env := setupEnv(t)
	env.startWorker(t, 5, 1)

	var attempts atomic.Int32
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	_, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules:      json.RawMessage(`{"type":"retry.test"}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	event, err := env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:   "retry.test",
		Source: "ci",
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	audit := waitForAuditStatus(t, env.eventService, event.ID, "success")
	if attempts.Load() < 2 {
		t.Fatalf("expected at least 2 webhook attempts, got %d", attempts.Load())
	}
	if audit.Attempts[0].AttemptNumber < 2 {
		t.Fatalf("expected attempt_number >= 2, got %d", audit.Attempts[0].AttemptNumber)
	}
}

func TestClientErrorNoRetry(t *testing.T) {
	env := setupEnv(t)
	env.startWorker(t, 5, 1)

	var attempts atomic.Int32
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer webhook.Close()

	_, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules:      json.RawMessage(`{"type":"client.error.test"}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	event, err := env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:   "client.error.test",
		Source: "ci",
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	audit := waitForAuditStatus(t, env.eventService, event.ID, "failed")
	if attempts.Load() != 1 {
		t.Fatalf("expected exactly 1 webhook attempt, got %d", attempts.Load())
	}
	if audit.Attempts[0].HTTPCode == nil || *audit.Attempts[0].HTTPCode != 400 {
		t.Fatalf("expected http 400 in audit")
	}
}

func TestSubscriptionAudit(t *testing.T) {
	env := setupEnv(t)
	env.startWorker(t, 3, 1)

	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	sub, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules:      json.RawMessage(`{"type":"audit.test"}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	event, err := env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:   "audit.test",
		Source: "ci",
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	waitForAuditStatus(t, env.eventService, event.ID, "success")

	views, err := env.eventService.GetSubscriptionAudit(env.ctx, sub.ID, 10, 0)
	if err != nil {
		t.Fatalf("subscription audit: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("expected subscription audit attempts")
	}
	if views[0].Status != "success" {
		t.Fatalf("expected success, got %s", views[0].Status)
	}
}

func TestSubscriptionCRUD(t *testing.T) {
	env := setupEnv(t)

	sub, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: "http://example.com/hook",
		Rules:      json.RawMessage(`{"type":"crud.test"}`),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := env.subService.GetSubscription(env.ctx, sub.ID)
	if err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if got.ID != sub.ID {
		t.Fatal("subscription not found after create")
	}

	subs, err := env.subService.ListSubscriptions(env.ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	got, err := env.subService.GetSubscription(env.ctx, sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WebhookURL != sub.WebhookURL {
		t.Fatal("webhook url mismatch")
	}

	updated, err := env.subService.UpdateSubscription(env.ctx, sub.ID, &models.CreateSubscriptionRequest{
		WebhookURL: "http://example.com/new",
		Rules:      json.RawMessage(`{"type":"crud.updated"}`),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.WebhookURL != "http://example.com/new" {
		t.Fatal("update not applied")
	}

	if err := env.subService.DeleteSubscription(env.ctx, sub.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	subs, err = env.subService.ListSubscriptions(env.ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 active subscriptions after delete, got %d", len(subs))
	}
}

func TestPayloadRuleFiltering(t *testing.T) {
	env := setupEnv(t)
	env.startWorker(t, 3, 1)

	called := make(chan struct{}, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	_, err := env.subService.CreateSubscription(env.ctx, &models.CreateSubscriptionRequest{
		WebhookURL: webhook.URL,
		Rules: json.RawMessage(`{
			"type":"payload.test",
			"payload_rules":[{"path":"$.tier","op":"==","value":"premium"}]
		}`),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// Non-matching payload
	_, err = env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:    "payload.test",
		Source:  "ci",
		Payload: json.RawMessage(`{"tier":"free"}`),
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	time.Sleep(2 * time.Second)
	select {
	case <-called:
		t.Fatal("should not deliver non-matching payload")
	default:
	}

	// Matching payload
	_, err = env.eventService.IngestEvent(env.ctx, &models.CreateEventRequest{
		Type:    "payload.test",
		Source:  "ci",
		Payload: json.RawMessage(`{"tier":"premium"}`),
	})
	if err != nil {
		t.Fatalf("ingest matching: %v", err)
	}

	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for matching payload delivery")
	}
}
