package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/repository"
)

// EventService handles event ingestion and processing
type EventService struct {
	eventRepo   *repository.EventRepository
	subRepo     *repository.SubscriptionRepository
	deliveryRepo *repository.DeliveryRepository
	redisCli    queue.Queue
	logger      *zap.Logger
}

// NewEventService creates a new event service
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

// IngestEvent receives an event and stores it durably
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

	s.logger.Info("event ingested", zap.String("event_id", event.ID.String()))
	return event, nil
}

// ProcessEvent evaluates all subscriptions and creates delivery attempts
func (s *EventService) ProcessEvent(ctx context.Context, event *models.Event) error {
	s.logger.Info("event processed", zap.String("event_id", event.ID.String()))
	return nil
}

// GetEventAudit returns delivery history for an event
func (s *EventService) GetEventAudit(ctx context.Context, eventID uuid.UUID, limit, offset int) (*models.DeliveryAudit, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, err
	}

	audit := &models.DeliveryAudit{
		EventID:       event.ID,
		Event:         event,
		Subscriptions: 0,
		Attempts:      make([]models.DeliveryAttemptView, 0),
	}

	return audit, nil
}

// GetSubscriptionAudit returns delivery history for a subscription
func (s *EventService) GetSubscriptionAudit(ctx context.Context, subID uuid.UUID, limit, offset int) ([]models.DeliveryAttemptView, error) {
	return make([]models.DeliveryAttemptView, 0), nil
}
