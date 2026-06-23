package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/repository"
)

// EventService handles event ingestion and audit queries.
type EventService struct {
	eventRepo    *repository.EventRepository
	subRepo      *repository.SubscriptionRepository
	deliveryRepo *repository.DeliveryRepository
	redisCli     queue.Queue
	logger       *zap.Logger
}

// NewEventService creates a new event service.
func NewEventService(
	eventRepo *repository.EventRepository,
	subRepo *repository.SubscriptionRepository,
	deliveryRepo *repository.DeliveryRepository,
	redisCli queue.Queue,
	logger *zap.Logger,
) *EventService {
	return &EventService{
		eventRepo:    eventRepo,
		subRepo:      subRepo,
		deliveryRepo: deliveryRepo,
		redisCli:     redisCli,
		logger:       logger,
	}
}

// IngestEvent receives an event, stores it durably, and enqueues it for fanout.
func (s *EventService) IngestEvent(ctx context.Context, req *models.CreateEventRequest) (*models.Event, error) {
	event := &models.Event{
		ID:        uuid.New(),
		Type:      req.Type,
		Source:    req.Source,
		Payload:   req.Payload,
		CreatedAt: time.Now(),
	}

	if err := s.eventRepo.Create(ctx, event); err != nil {
		s.logger.Error("failed to store event in database", zap.Error(err))
		return nil, err
	}

	if s.redisCli != nil {
		if err := s.redisCli.EnqueueEvent(ctx, event); err != nil {
			s.logger.Error("failed to enqueue event", zap.Error(err))
			return nil, fmt.Errorf("enqueue event: %w", err)
		}
	}

	s.logger.Info("event ingested", zap.String("event_id", event.ID.String()))
	return event, nil
}

// GetEvent retrieves an event by ID.
func (s *EventService) GetEvent(ctx context.Context, eventID uuid.UUID) (*models.Event, error) {
	return s.eventRepo.GetByID(ctx, eventID)
}

// GetEventAudit returns delivery history for an event.
func (s *EventService) GetEventAudit(ctx context.Context, eventID uuid.UUID, limit, offset int) (*models.DeliveryAudit, error) {
	if limit <= 0 {
		limit = 50
	}

	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, err
	}

	attempts, err := s.deliveryRepo.ListByEventID(ctx, eventID, limit, offset)
	if err != nil {
		return nil, err
	}

	subs, err := s.loadSubscriptionsForAttempts(ctx, attempts)
	if err != nil {
		return nil, err
	}

	return &models.DeliveryAudit{
		EventID:       event.ID,
		Event:         event,
		Subscriptions: len(subs),
		Attempts:      BuildAuditViews(attempts, subs),
	}, nil
}

// GetSubscriptionAudit returns delivery history for a subscription.
func (s *EventService) GetSubscriptionAudit(ctx context.Context, subID uuid.UUID, limit, offset int) ([]models.DeliveryAttemptView, error) {
	if limit <= 0 {
		limit = 50
	}

	sub, err := s.subRepo.GetByID(ctx, subID)
	if err != nil {
		return nil, err
	}

	attempts, err := s.deliveryRepo.ListBySubscriptionID(ctx, subID, limit, offset)
	if err != nil {
		return nil, err
	}

	subs := map[uuid.UUID]models.Subscription{sub.ID: *sub}
	return BuildAuditViews(attempts, subs), nil
}

func (s *EventService) loadSubscriptionsForAttempts(ctx context.Context, attempts []models.DeliveryAttempt) (map[uuid.UUID]models.Subscription, error) {
	subs := make(map[uuid.UUID]models.Subscription)
	for _, attempt := range attempts {
		if _, ok := subs[attempt.SubscriptionID]; ok {
			continue
		}
		sub, err := s.subRepo.GetByID(ctx, attempt.SubscriptionID)
		if err != nil {
			continue
		}
		subs[attempt.SubscriptionID] = *sub
	}
	return subs, nil
}
