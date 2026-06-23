package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewDBPool creates a new PostgreSQL connection pool
func NewDBPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	return pool, nil
}

// RunMigrations runs SQL migrations from a provided SQL file
func RunMigrations(ctx context.Context, db *pgxpool.Pool, migrationSQL string) error {
	_, err := db.Exec(ctx, migrationSQL)
	return err
}
