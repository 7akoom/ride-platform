package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRevocationStore struct {
	pool *pgxpool.Pool
}

var _ auth.SessionRevocationStore = (*SessionRevocationStore)(nil)

func NewSessionRevocationStore(
	pool *pgxpool.Pool,
) *SessionRevocationStore {
	return &SessionRevocationStore{
		pool: pool,
	}
}

func (s *SessionRevocationStore) RevokeByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	if refreshTokenHash == "" {
		return auth.ErrInvalidRefreshToken
	}

	if revokedAt.IsZero() {
		return errors.New(
			"session revocation time cannot be zero",
		)
	}

	revokedAt = revokedAt.UTC()

	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"begin session revocation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const selectSessionQuery = `
		SELECT s.id::text
		FROM refresh_tokens AS rt
		INNER JOIN auth_sessions AS s
			ON s.id = rt.session_id
		WHERE rt.token_hash = $1
		FOR UPDATE OF rt, s
	`

	var sessionID string

	err = tx.QueryRow(
		ctx,
		selectSessionQuery,
		refreshTokenHash,
	).Scan(
		&sessionID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf(
			"find session by refresh token hash: %w",
			err,
		)
	}

	if err := revokeSessionInTransaction(
		ctx,
		tx,
		sessionID,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"revoke authentication session: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit session revocation: %w",
			err,
		)
	}

	return nil
}