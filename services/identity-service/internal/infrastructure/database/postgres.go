package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const databasePingTimeout = 5 * time.Second

func NewPostgresPool(
	ctx context.Context,
	databaseURL string,
) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errors.New("database URL cannot be empty")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL connection pool: %w", err)
	}

	pingContext, cancel := context.WithTimeout(
		ctx,
		databasePingTimeout,
	)
	defer cancel()

	if err := pool.Ping(pingContext); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return pool, nil
}