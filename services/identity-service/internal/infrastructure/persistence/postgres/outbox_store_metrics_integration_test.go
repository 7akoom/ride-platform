//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutboxPendingStats(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	cfg.MaxConns = 1
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create database pool failed")
	}
	defer pool.Close()
	// A connection-local table keeps the test independent of existing outbox data.
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE outbox_events (
		occurred_at timestamptz NOT NULL, published_at timestamptz,
		available_at timestamptz, claimed_at timestamptz, publish_attempts integer)`)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			t.Fatalf("create temporary outbox table failed (SQLSTATE %s)", pgErr.Code)
		}
		t.Fatalf("create temporary outbox table failed (type %T, connection refused: %t)", err, errors.Is(err, syscall.ECONNREFUSED))
	}
	store := NewOutboxStore(pool)
	count, age, err := store.PendingStats(ctx)
	if err != nil || count != 0 || age != 0 {
		t.Fatal("empty outbox must report zero count and age")
	}
	_, err = pool.Exec(ctx, `INSERT INTO outbox_events VALUES
		(now() - interval '1 day', now(), now(), NULL, 1),
		(now() - interval '2 minutes', NULL, now(), NULL, 0),
		(now() - interval '5 minutes', NULL, now() + interval '1 minute', NULL, 3),
		(now() - interval '3 minutes', NULL, now() + interval '1 minute', now(), 1)`)
	if err != nil {
		t.Fatal("insert test outbox events failed")
	}
	count, age, err = store.PendingStats(ctx)
	if err != nil || count != 3 || age < 300 || age > 310 {
		t.Fatal("pending statistics must include retries and leases, excluding published events")
	}
	_, err = pool.Exec(ctx, `UPDATE outbox_events SET published_at = now()`)
	if err != nil {
		t.Fatal("mark temporary events published failed")
	}
	count, age, err = store.PendingStats(ctx)
	if err != nil || count != 0 || age != 0 {
		t.Fatal("drained outbox must report zero count and age")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.PendingStats(cancelCtx); err == nil {
		t.Fatal("expected canceled query to fail")
	}
}
