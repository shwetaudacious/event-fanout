package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

func TestRulesMatcher_TypeAndSource(t *testing.T) {
	matcher := NewRulesMatcher()

	event := &models.Event{
		ID:     uuid.New(),
		Type:   "user.created",
		Source: "auth-service",
	}

	sub := &models.Subscription{
		Rules: json.RawMessage(`{"type":"user.created","source":"auth-service"}`),
	}

	match, err := matcher.Matches(event, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Fatal("expected match")
	}
}

func TestRulesMatcher_WildcardType(t *testing.T) {
	matcher := NewRulesMatcher()
	event := &models.Event{Type: "user.deleted", Source: "auth-service"}
	sub := &models.Subscription{
		Rules: json.RawMessage(`{"type":"user.*"}`),
	}

	match, err := matcher.Matches(event, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Fatal("expected wildcard match")
	}
}

func TestRulesMatcher_PayloadRuleEquals(t *testing.T) {
	matcher := NewRulesMatcher()
	event := &models.Event{
		Type:    "order.created",
		Source:  "billing",
		Payload: json.RawMessage(`{"amount":1500,"currency":"USD"}`),
	}
	sub := &models.Subscription{
		Rules: json.RawMessage(`{
			"type":"order.created",
			"payload_rules":[{"path":"$.currency","op":"==","value":"USD"}]
		}`),
	}

	match, err := matcher.Matches(event, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Fatal("expected payload rule match")
	}
}

func TestRulesMatcher_PayloadRuleGreaterThan(t *testing.T) {
	matcher := NewRulesMatcher()
	event := &models.Event{
		Type:    "order.created",
		Payload: json.RawMessage(`{"amount":1500}`),
	}
	sub := &models.Subscription{
		Rules: json.RawMessage(`{
			"payload_rules":[{"path":"$.amount","op":">","value":1000}]
		}`),
	}

	match, err := matcher.Matches(event, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Fatal("expected numeric comparison match")
	}
}

func TestRulesMatcher_NoMatch(t *testing.T) {
	matcher := NewRulesMatcher()
	event := &models.Event{Type: "user.created", Source: "auth-service"}
	sub := &models.Subscription{
		Rules: json.RawMessage(`{"type":"invoice.paid"}`),
	}

	match, err := matcher.Matches(event, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Fatal("expected no match")
	}
}

func TestRulesMatcher_RegexPayloadRule(t *testing.T) {
	matcher := NewRulesMatcher()
	event := &models.Event{
		Payload: json.RawMessage(`{"email":"admin@example.com"}`),
	}
	sub := &models.Subscription{
		Rules: json.RawMessage(`{
			"payload_rules":[{"path":"$.email","op":"regex","value":".*@example\\.com$"}]
		}`),
	}

	match, err := matcher.Matches(event, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Fatal("expected regex match")
	}
}
