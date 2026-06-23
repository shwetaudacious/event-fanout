package service

import (
	"encoding/json"
	"fmt"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

// RulesMatcher evaluates whether an event matches subscription rules
type RulesMatcher struct{}

// NewRulesMatcher creates a new rules matcher
func NewRulesMatcher() *RulesMatcher {
	return &RulesMatcher{}
}

// Matches checks if an event matches subscription rules
func (m *RulesMatcher) Matches(event *models.Event, subscription *models.Subscription) (bool, error) {
	var rules models.SubscriptionRules
	if err := json.Unmarshal(subscription.Rules, &rules); err != nil {
		return false, fmt.Errorf("failed to parse subscription rules: %w", err)
	}

	// Check event type
	if rules.Type != "" && rules.Type != event.Type {
		return false, nil
	}

	// Check event source
	if rules.Source != "" && rules.Source != event.Source {
		return false, nil
	}

	return true, nil
}
