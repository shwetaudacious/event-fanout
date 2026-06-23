package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

// DeliveryRepository handles database operations for delivery attempts
type DeliveryRepository struct {
	db *pgxpool.Pool
}

// NewDeliveryRepository creates a new delivery repository
func NewDeliveryRepository(db *pgxpool.Pool) *DeliveryRepository {
	return &DeliveryRepository{db: db}
}

// Create inserts a new delivery attempt
func (r *DeliveryRepository) Create(ctx context.Context, attempt *models.DeliveryAttempt) error {
	query := `
		INSERT INTO delivery_attempts 
		(id, event_id, subscription_id, attempt_number, status, http_code, error_message, next_retry_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (event_id, subscription_id) DO UPDATE
		SET attempt_number = delivery_attempts.attempt_number + 1,
		    status = $5,
		    http_code = $6,
		    error_message = $7,
		    next_retry_at = $8,
		    created_at = NOW()
	`
	_, err := r.db.Exec(ctx, query,
		attempt.ID, attempt.EventID, attempt.SubscriptionID,
		attempt.AttemptNumber, attempt.Status, attempt.HTTPCode,
		attempt.ErrorMessage, attempt.NextRetryAt, attempt.CreatedAt,
	)
	return err
}

// GetByID retrieves a delivery attempt by ID
func (r *DeliveryRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.DeliveryAttempt, error) {
	query := `
		SELECT id, event_id, subscription_id, attempt_number, status, http_code, error_message, next_retry_at, created_at
		FROM delivery_attempts
		WHERE id = $1
	`
	attempt := &models.DeliveryAttempt{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&attempt.ID, &attempt.EventID, &attempt.SubscriptionID,
		&attempt.AttemptNumber, &attempt.Status, &attempt.HTTPCode,
		&attempt.ErrorMessage, &attempt.NextRetryAt, &attempt.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("delivery attempt not found: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return attempt, nil
}

// ListByEventID retrieves all delivery attempts for an event
func (r *DeliveryRepository) ListByEventID(ctx context.Context, eventID uuid.UUID, limit, offset int) ([]models.DeliveryAttempt, error) {
	query := `
		SELECT id, event_id, subscription_id, attempt_number, status, http_code, error_message, next_retry_at, created_at
		FROM delivery_attempts
		WHERE event_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, eventID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []models.DeliveryAttempt
	for rows.Next() {
		var attempt models.DeliveryAttempt
		if err := rows.Scan(
			&attempt.ID, &attempt.EventID, &attempt.SubscriptionID,
			&attempt.AttemptNumber, &attempt.Status, &attempt.HTTPCode,
			&attempt.ErrorMessage, &attempt.NextRetryAt, &attempt.CreatedAt,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

// ListBySubscriptionID retrieves all delivery attempts for a subscription
func (r *DeliveryRepository) ListBySubscriptionID(ctx context.Context, subID uuid.UUID, limit, offset int) ([]models.DeliveryAttempt, error) {
	query := `
		SELECT id, event_id, subscription_id, attempt_number, status, http_code, error_message, next_retry_at, created_at
		FROM delivery_attempts
		WHERE subscription_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, subID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []models.DeliveryAttempt
	for rows.Next() {
		var attempt models.DeliveryAttempt
		if err := rows.Scan(
			&attempt.ID, &attempt.EventID, &attempt.SubscriptionID,
			&attempt.AttemptNumber, &attempt.Status, &attempt.HTTPCode,
			&attempt.ErrorMessage, &attempt.NextRetryAt, &attempt.CreatedAt,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

// ListPendingRetries retrieves delivery attempts that should be retried
func (r *DeliveryRepository) ListPendingRetries(ctx context.Context, limit int) ([]models.DeliveryAttempt, error) {
	query := `
		SELECT id, event_id, subscription_id, attempt_number, status, http_code, error_message, next_retry_at, created_at
		FROM delivery_attempts
		WHERE (status = 'pending' OR (status = 'failed' AND next_retry_at <= NOW()))
		ORDER BY next_retry_at ASC NULLS FIRST
		LIMIT $1
	`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []models.DeliveryAttempt
	for rows.Next() {
		var attempt models.DeliveryAttempt
		if err := rows.Scan(
			&attempt.ID, &attempt.EventID, &attempt.SubscriptionID,
			&attempt.AttemptNumber, &attempt.Status, &attempt.HTTPCode,
			&attempt.ErrorMessage, &attempt.NextRetryAt, &attempt.CreatedAt,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

// UpdateStatus updates the status of a delivery attempt
func (r *DeliveryRepository) UpdateStatus(ctx context.Context, attemptID uuid.UUID, status string, httpCode *int, errMsg *string, nextRetry *time.Time) error {
	query := `
		UPDATE delivery_attempts
		SET status = $1, http_code = $2, error_message = $3, next_retry_at = $4
		WHERE id = $5
	`
	_, err := r.db.Exec(ctx, query, status, httpCode, errMsg, nextRetry, attemptID)
	return err
}

// GetOrCreateDeliveryAttempt gets or creates a delivery attempt
func (r *DeliveryRepository) GetOrCreateDeliveryAttempt(ctx context.Context, eventID, subID uuid.UUID) (*models.DeliveryAttempt, error) {
	query := `
		SELECT id, event_id, subscription_id, attempt_number, status, http_code, error_message, next_retry_at, created_at
		FROM delivery_attempts
		WHERE event_id = $1 AND subscription_id = $2
	`
	attempt := &models.DeliveryAttempt{}
	err := r.db.QueryRow(ctx, query, eventID, subID).Scan(
		&attempt.ID, &attempt.EventID, &attempt.SubscriptionID,
		&attempt.AttemptNumber, &attempt.Status, &attempt.HTTPCode,
		&attempt.ErrorMessage, &attempt.NextRetryAt, &attempt.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		// Create new attempt
		newAttempt := &models.DeliveryAttempt{
			ID:             uuid.New(),
			EventID:        eventID,
			SubscriptionID: subID,
			AttemptNumber:  1,
			Status:         "pending",
			CreatedAt:      time.Now(),
		}
		if err := r.Create(ctx, newAttempt); err != nil {
			return nil, err
		}
		return newAttempt, nil
	}

	if err != nil {
		return nil, err
	}
	return attempt, nil
}
