package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/share"
)

// ShareStore keeps trip sharing links.
type ShareStore struct {
	pool *pgxpool.Pool
}

var _ share.Store = (*ShareStore)(nil)

func NewShareStore(pool *pgxpool.Pool) *ShareStore {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &ShareStore{pool: pool}
}

const shareColumns = `id, trip_id, token_hash, created_at, expires_at, revoked_at`

func scanShare(row pgx.Row) (share.Link, error) {
	var link share.Link

	err := row.Scan(&link.ID, &link.TripID, &link.TokenHash, &link.CreatedAt, &link.ExpiresAt, &link.RevokedAt)

	return link, err
}

func (s *ShareStore) Create(ctx context.Context, link share.Link, maxLive int) (share.Link, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return share.Link{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The trip's links are counted and added one at a time.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('trip_shares:' || $1))`, link.TripID); err != nil {
		return share.Link{}, fmt.Errorf("lock the trip's links: %w", err)
	}

	var live int
	if err := tx.QueryRow(
		ctx,
		`SELECT count(*) FROM trip_shares WHERE trip_id = $1 AND revoked_at IS NULL AND expires_at > $2`,
		link.TripID, link.CreatedAt,
	).Scan(&live); err != nil {
		return share.Link{}, fmt.Errorf("count live links: %w", err)
	}

	if live >= maxLive {
		return share.Link{}, share.ErrTooManyLinks
	}

	created, err := scanShare(tx.QueryRow(
		ctx,
		`INSERT INTO trip_shares (trip_id, token_hash, created_at, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+shareColumns,
		link.TripID, link.TokenHash, link.CreatedAt, link.ExpiresAt,
	))
	if err != nil {
		return share.Link{}, fmt.Errorf("insert link: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return share.Link{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (s *ShareStore) FindLive(ctx context.Context, tokenHash []byte, now time.Time) (share.Link, error) {
	found, err := scanShare(s.pool.QueryRow(
		ctx,
		`SELECT `+shareColumns+` FROM trip_shares
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2`,
		tokenHash, now,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return share.Link{}, share.ErrNotFound
	case err != nil:
		return share.Link{}, fmt.Errorf("select link: %w", err)
	}

	return found, nil
}

func (s *ShareStore) RevokeAll(ctx context.Context, tripID string, now time.Time) (int, error) {
	tag, err := s.pool.Exec(
		ctx,
		`UPDATE trip_shares SET revoked_at = $2 WHERE trip_id = $1 AND revoked_at IS NULL AND expires_at > $2`,
		tripID, now,
	)
	if err != nil {
		return 0, fmt.Errorf("revoke links: %w", err)
	}

	return int(tag.RowsAffected()), nil
}
