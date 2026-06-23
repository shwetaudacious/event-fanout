package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/event-fanout-service/event-fanout/internal/models"
)

// SubscriptionRepository handles database operations for subscriptions
type SubscriptionRepository struct {
	db *pgxpool.Pool
}

// NewSubscriptionRepository creates a new subscription repository
func NewSubscriptionRepository(db *pgxpool.Pool) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

// Create inserts a new subscription
func (r *SubscriptionRepository) Create(ctx context.Context, sub *models.Subscription) error {
	query := `
		INSERT INTO subscriptions (id, webhook_url, rules, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(ctx, query, sub.ID, sub.WebhookURL, sub.Rules, sub.Active, sub.CreatedAt, sub.UpdatedAt)
	return err
}

// GetByID retrieves a subscription by ID
func (r *SubscriptionRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Subscription, error) {
	query := `
		SELECT id, webhook_url, rules, active, created_at, updated_at
		FROM subscriptions
		WHERE id = $1
	`
	sub := &models.Subscription{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&sub.ID, &sub.WebhookURL, &sub.Rules, &sub.Active, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("subscription not found: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// ListAll retrieves all active subscriptions
func (r *SubscriptionRepository) ListAll(ctx context.Context) ([]models.Subscription, error) {
	query := `
		SELECT id, webhook_url, rules, active, created_at, updated_at
		FROM subscriptions
		WHERE active = true
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var sub models.Subscription
		if err := rows.Scan(&sub.ID, &sub.WebhookURL, &sub.Rules, &sub.Active, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// Update modifies an existing subscription
func (r *SubscriptionRepository) Update(ctx context.Context, sub *models.Subscription) error {
	query := `
		UPDATE subscriptions
		SET webhook_url = $1, rules = $2, active = $3, updated_at = $4
		WHERE id = $5
	`
	_, err := r.db.Exec(ctx, query, sub.WebhookURL, sub.Rules, sub.Active, sub.UpdatedAt, sub.ID)
	return err
}

// Delete marks a subscription as inactive
func (r *SubscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE subscriptions
		SET active = false, updated_at = NOW()
		WHERE id = $1
	`
	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("subscription not found")
	}
	return nil
}
