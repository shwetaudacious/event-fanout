package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

// RulesMatcher evaluates whether an event matches subscription rules.
type RulesMatcher struct{}

// NewRulesMatcher creates a new rules matcher.
func NewRulesMatcher() *RulesMatcher {
	return &RulesMatcher{}
}

// Matches checks if an event matches subscription rules.
func (m *RulesMatcher) Matches(event *models.Event, subscription *models.Subscription) (bool, error) {
	var rules models.SubscriptionRules
	if err := json.Unmarshal(subscription.Rules, &rules); err != nil {
		return false, fmt.Errorf("failed to parse subscription rules: %w", err)
	}

	if rules.Type != "" && !matchPattern(rules.Type, event.Type) {
		return false, nil
	}

	if rules.Source != "" && !matchPattern(rules.Source, event.Source) {
		return false, nil
	}

	for _, rule := range rules.PayloadRules {
		ok, err := m.evaluatePayloadRule(event.Payload, rule)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}

func matchPattern(pattern, value string) bool {
	if pattern == "" {
		return true
	}
	if strings.Contains(pattern, "*") {
		rePattern := strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*")
		re, err := regexp.Compile("^" + rePattern + "$")
		if err != nil {
			return pattern == value
		}
		return re.MatchString(value)
	}
	return pattern == value
}

func (m *RulesMatcher) evaluatePayloadRule(payload json.RawMessage, rule models.PayloadRule) (bool, error) {
	var data interface{}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return false, fmt.Errorf("invalid event payload: %w", err)
	}

	actual, ok := extractJSONPath(data, rule.Path)
	if !ok {
		return false, nil
	}

	return compareValues(actual, rule.Op, rule.Value)
}

func extractJSONPath(data interface{}, jsonPath string) (interface{}, bool) {
	if jsonPath == "" || jsonPath == "$" {
		return data, true
	}

	clean := strings.TrimPrefix(jsonPath, "$.")
	clean = strings.TrimPrefix(clean, "$")
	if clean == "" {
		return data, true
	}

	parts := strings.Split(clean, ".")
	current := data
	for _, part := range parts {
		switch typed := current.(type) {
		case map[string]interface{}:
			val, exists := typed[part]
			if !exists {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}
	return current, true
}

func compareValues(actual interface{}, op string, expected interface{}) (bool, error) {
	switch op {
	case "==":
		return valuesEqual(actual, expected), nil
	case "!=":
		return !valuesEqual(actual, expected), nil
	case ">", "<", ">=", "<=":
		return compareNumbers(actual, expected, op)
	case "in":
		return valueIn(actual, expected)
	case "regex":
		pattern, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("regex operator requires string pattern")
		}
		actualStr := fmt.Sprintf("%v", actual)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("invalid regex: %w", err)
		}
		return re.MatchString(actualStr), nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", op)
	}
}

func valuesEqual(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func compareNumbers(actual, expected interface{}, op string) (bool, error) {
	a, err := toFloat64(actual)
	if err != nil {
		return false, err
	}
	b, err := toFloat64(expected)
	if err != nil {
		return false, err
	}
	switch op {
	case ">":
		return a > b, nil
	case "<":
		return a < b, nil
	case ">=":
		return a >= b, nil
	case "<=":
		return a <= b, nil
	default:
		return false, fmt.Errorf("unsupported numeric operator: %s", op)
	}
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	default:
		return 0, fmt.Errorf("value %v is not numeric", v)
	}
}

func valueIn(actual, expected interface{}) (bool, error) {
	items, ok := expected.([]interface{})
	if !ok {
		return false, fmt.Errorf("in operator requires array value")
	}
	for _, item := range items {
		if valuesEqual(actual, item) {
			return true, nil
		}
	}
	return false, nil
}
