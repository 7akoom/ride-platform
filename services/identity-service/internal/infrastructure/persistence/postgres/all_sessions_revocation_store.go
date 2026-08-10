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

type AllSessionsRevocationStore struct {
	pool *pgxpool.Pool
}

var _ auth.AllSessionsRevocationStore = (*AllSessionsRevocationStore)(nil)

func NewAllSessionsRevocationStore(
	pool *pgxpool.Pool,
) *AllSessionsRevocationStore {
	return &AllSessionsRevocationStore{
		pool: pool,
	}
}

func (s *AllSessionsRevocationStore) RevokeAllByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	if refreshTokenHash == "" {
		return auth.ErrInvalidRefreshToken
	}

	if revokedAt.IsZero() {
		return errors.New(
			"all sessions revocation time cannot be zero",
		)
	}

	revokedAt = revokedAt.UTC()

	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"begin all sessions revocation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const selectIdentityQuery = `
		SELECT s.identity_id::text
		FROM refresh_tokens AS rt
		INNER JOIN auth_sessions AS s
			ON s.id = rt.session_id
		WHERE rt.token_hash = $1
		  AND rt.used_at IS NULL
		  AND rt.revoked_at IS NULL
		  AND rt.expires_at > $2
		  AND s.revoked_at IS NULL
		  AND s.expires_at > $2
		FOR UPDATE OF rt, s
	`

	var identityID string

	err = tx.QueryRow(
		ctx,
		selectIdentityQuery,
		refreshTokenHash,
		revokedAt,
	).Scan(
		&identityID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf(
			"find active identity session by refresh token hash: %w",
			err,
		)
	}

	const revokeSessionsQuery = `
		UPDATE auth_sessions
		SET
			revoked_at = COALESCE(
				revoked_at,
				$1
			),
			updated_at = $1
		WHERE identity_id = $2::uuid
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeSessionsQuery,
		revokedAt,
		identityID,
	); err != nil {
		return fmt.Errorf(
			"revoke identity authentication sessions: %w",
			err,
		)
	}

	const revokeRefreshTokensQuery = `
		UPDATE refresh_tokens
		SET revoked_at = COALESCE(
			revoked_at,
			$1
		)
		WHERE session_id IN (
			SELECT id
			FROM auth_sessions
			WHERE identity_id = $2::uuid
		)
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeRefreshTokensQuery,
		revokedAt,
		identityID,
	); err != nil {
		return fmt.Errorf(
			"revoke identity refresh tokens: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit all sessions revocation: %w",
			err,
		)
	}

	return nil
}