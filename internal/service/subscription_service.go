package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/repository"
)

// SubscriptionService manages subscriptions
type SubscriptionService struct {
	subRepo *repository.SubscriptionRepository
	logger  *zap.Logger
}

// NewSubscriptionService creates a new subscription service
func NewSubscriptionService(subRepo *repository.SubscriptionRepository, logger *zap.Logger) *SubscriptionService {
	return &SubscriptionService{
		subRepo: subRepo,
		logger:  logger,
	}
}

// CreateSubscription creates a new subscription
func (s *SubscriptionService) CreateSubscription(ctx context.Context, req *models.CreateSubscriptionRequest) (*models.Subscription, error) {
	sub := &models.Subscription{
		ID:         uuid.New(),
		WebhookURL: req.WebhookURL,
		Rules:      req.Rules,
		Active:     true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := s.subRepo.Create(ctx, sub); err != nil {
		s.logger.Error("failed to create subscription", zap.Error(err))
		return nil, err
	}

	s.logger.Info("subscription created", zap.String("sub_id", sub.ID.String()))
	return sub, nil
}

// GetSubscription retrieves a subscription by ID
func (s *SubscriptionService) GetSubscription(ctx context.Context, id uuid.UUID) (*models.Subscription, error) {
	return s.subRepo.GetByID(ctx, id)
}

// ListSubscriptions retrieves all active subscriptions
func (s *SubscriptionService) ListSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	return s.subRepo.ListAll(ctx)
}

// UpdateSubscription updates an existing subscription
func (s *SubscriptionService) UpdateSubscription(ctx context.Context, id uuid.UUID, req *models.CreateSubscriptionRequest) (*models.Subscription, error) {
	sub, err := s.subRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	sub.WebhookURL = req.WebhookURL
	sub.Rules = req.Rules
	sub.UpdatedAt = time.Now()

	if err := s.subRepo.Update(ctx, sub); err != nil {
		s.logger.Error("failed to update subscription", zap.String("sub_id", id.String()), zap.Error(err))
		return nil, err
	}

	s.logger.Info("subscription updated", zap.String("sub_id", id.String()))
	return sub, nil
}

// DeleteSubscription deletes a subscription
func (s *SubscriptionService) DeleteSubscription(ctx context.Context, id uuid.UUID) error {
	if err := s.subRepo.Delete(ctx, id); err != nil {
		s.logger.Error("failed to delete subscription", zap.String("sub_id", id.String()), zap.Error(err))
		return err
	}

	s.logger.Info("subscription deleted", zap.String("sub_id", id.String()))
	return nil
}
