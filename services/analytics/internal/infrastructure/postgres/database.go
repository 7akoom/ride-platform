package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const databasePingTimeout = 5 * time.Second

// NewPool opens a connection pool to analytics_db and registers the
// shopspring/decimal type handlers on every connection — same pattern as
// wallet-service — so NUMERIC columns (revenue_daily's money fields) scan
// directly into decimal.Decimal instead of losing precision through
// float64.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errors.New("database URL cannot be empty")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxdecimal.Register(conn.TypeMap())

		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL connection pool: %w", err)
	}

	pingContext, cancel := context.WithTimeout(ctx, databasePingTimeout)
	defer cancel()

	if err := pool.Ping(pingContext); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return pool, nil
}
