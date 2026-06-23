package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

// EventRepository handles database operations for events
type EventRepository struct {
	db *pgxpool.Pool
}

// NewEventRepository creates a new event repository
func NewEventRepository(db *pgxpool.Pool) *EventRepository {
	return &EventRepository{db: db}
}

// Create inserts a new event into the database
func (r *EventRepository) Create(ctx context.Context, event *models.Event) error {
	query := `
		INSERT INTO events (id, type, source, payload, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.Exec(ctx, query, event.ID, event.Type, event.Source, event.Payload, event.CreatedAt)
	return err
}

// GetByID retrieves an event by ID
func (r *EventRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Event, error) {
	query := `
		SELECT id, type, source, payload, created_at
		FROM events
		WHERE id = $1
	`
	event := &models.Event{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&event.ID, &event.Type, &event.Source, &event.Payload, &event.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("event not found: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return event, nil
}

// ListByType retrieves all events of a specific type
func (r *EventRepository) ListByType(ctx context.Context, eventType string, limit, offset int) ([]models.Event, error) {
	query := `
		SELECT id, type, source, payload, created_at
		FROM events
		WHERE type = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, eventType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var event models.Event
		if err := rows.Scan(&event.ID, &event.Type, &event.Source, &event.Payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// ListAll retrieves all events with pagination
func (r *EventRepository) ListAll(ctx context.Context, limit, offset int) ([]models.Event, error) {
	query := `
		SELECT id, type, source, payload, created_at
		FROM events
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var event models.Event
		if err := rows.Scan(&event.ID, &event.Type, &event.Source, &event.Payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
