package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/delivery"
	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/repository"
)

// FanoutService handles event fanout, webhook delivery, and retries.
type FanoutService struct {
	eventRepo    *repository.EventRepository
	subRepo      *repository.SubscriptionRepository
	deliveryRepo *repository.DeliveryRepository
	matcher      *RulesMatcher
	webhook      *delivery.Client
	maxRetries   int
	baseDelay    time.Duration
	logger       *zap.Logger
}

// NewFanoutService creates a fanout service.
func NewFanoutService(
	eventRepo *repository.EventRepository,
	subRepo *repository.SubscriptionRepository,
	deliveryRepo *repository.DeliveryRepository,
	webhook *delivery.Client,
	maxRetries int,
	baseDelaySeconds int,
	logger *zap.Logger,
) *FanoutService {
	return &FanoutService{
		eventRepo:    eventRepo,
		subRepo:      subRepo,
		deliveryRepo: deliveryRepo,
		matcher:      NewRulesMatcher(),
		webhook:      webhook,
		maxRetries:   maxRetries,
		baseDelay:    time.Duration(baseDelaySeconds) * time.Second,
		logger:       logger,
	}
}

// ProcessEvent evaluates subscriptions and delivers matching events.
func (s *FanoutService) ProcessEvent(ctx context.Context, event *models.Event) error {
	subs, err := s.subRepo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}

	for i := range subs {
		sub := subs[i]
		match, err := s.matcher.Matches(event, &sub)
		if err != nil {
			s.logger.Warn("rule evaluation failed",
				zap.String("subscription_id", sub.ID.String()),
				zap.Error(err),
			)
			continue
		}
		if !match {
			continue
		}

		attempt, err := s.deliveryRepo.GetOrCreateDeliveryAttempt(ctx, event.ID, sub.ID)
		if err != nil {
			return fmt.Errorf("create delivery attempt: %w", err)
		}
		if attempt.Status == "success" {
			continue
		}

		if err := s.deliverAttempt(ctx, event, &sub, attempt); err != nil {
			s.logger.Error("delivery failed",
				zap.String("event_id", event.ID.String()),
				zap.String("subscription_id", sub.ID.String()),
				zap.Error(err),
			)
		}
	}

	return nil
}

// ProcessPendingRetries delivers attempts that are due for retry.
func (s *FanoutService) ProcessPendingRetries(ctx context.Context, limit int) error {
	attempts, err := s.deliveryRepo.ListPendingRetries(ctx, limit)
	if err != nil {
		return err
	}

	for i := range attempts {
		attempt := attempts[i]
		if attempt.Status == "success" {
			continue
		}

		event, err := s.eventRepo.GetByID(ctx, attempt.EventID)
		if err != nil {
			s.logger.Warn("event missing for retry", zap.String("event_id", attempt.EventID.String()))
			continue
		}

		sub, err := s.subRepo.GetByID(ctx, attempt.SubscriptionID)
		if err != nil || !sub.Active {
			continue
		}

		if err := s.deliverAttempt(ctx, event, sub, &attempt); err != nil {
			s.logger.Warn("retry delivery failed",
				zap.String("attempt_id", attempt.ID.String()),
				zap.Error(err),
			)
		}
	}

	return nil
}

func (s *FanoutService) deliverAttempt(ctx context.Context, event *models.Event, sub *models.Subscription, attempt *models.DeliveryAttempt) error {
	result := s.webhook.Deliver(ctx, sub.WebhookURL, event)
	now := time.Now()

	if result.Err != nil {
		return s.markFailure(ctx, attempt, nil, result.Err.Error(), now)
	}

	if delivery.IsSuccess(result.HTTPCode) {
		code := result.HTTPCode
		return s.deliveryRepo.UpdateStatus(ctx, attempt.ID, "success", &code, nil, nil)
	}

	if delivery.IsClientError(result.HTTPCode) {
		code := result.HTTPCode
		msg := fmt.Sprintf("client error: %d", result.HTTPCode)
		return s.deliveryRepo.UpdateStatus(ctx, attempt.ID, "failed", &code, &msg, nil)
	}

	code := result.HTTPCode
	msg := fmt.Sprintf("server error: %d", result.HTTPCode)
	return s.markFailure(ctx, attempt, &code, msg, now)
}

func (s *FanoutService) markFailure(ctx context.Context, attempt *models.DeliveryAttempt, httpCode *int, errMsg string, now time.Time) error {
	nextAttempt := attempt.AttemptNumber + 1
	if nextAttempt > s.maxRetries {
		return s.deliveryRepo.UpdateStatus(ctx, attempt.ID, "failed", httpCode, &errMsg, nil)
	}

	delay := time.Duration(math.Pow(2, float64(attempt.AttemptNumber-1))) * s.baseDelay
	nextRetry := now.Add(delay)
	status := "pending"
	if attempt.AttemptNumber >= 1 {
		status = "failed"
	}

	if err := s.deliveryRepo.IncrementAttempt(ctx, attempt.ID, status, httpCode, &errMsg, &nextRetry, nextAttempt); err != nil {
		return err
	}
	return fmt.Errorf("%s", errMsg)
}

// BuildAuditViews converts delivery attempts into audit views with webhook URLs.
func BuildAuditViews(attempts []models.DeliveryAttempt, subs map[uuid.UUID]models.Subscription) []models.DeliveryAttemptView {
	views := make([]models.DeliveryAttemptView, 0, len(attempts))
	for _, attempt := range attempts {
		view := models.DeliveryAttemptView{
			SubscriptionID: attempt.SubscriptionID,
			Status:         attempt.Status,
			AttemptNumber:  attempt.AttemptNumber,
			HTTPCode:       attempt.HTTPCode,
			ErrorMessage:   attempt.ErrorMessage,
			Timestamp:      attempt.CreatedAt,
		}
		if sub, ok := subs[attempt.SubscriptionID]; ok {
			view.WebhookURL = sub.WebhookURL
		}
		views = append(views, view)
	}
	return views
}
