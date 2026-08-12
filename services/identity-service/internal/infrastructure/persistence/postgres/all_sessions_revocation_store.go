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

var _ auth.AllSessionsRevocationTargetStore = (*AllSessionsRevocationStore)(nil)

var _ auth.AllSessionsPersistentRevocationStore = (*AllSessionsRevocationStore)(nil)

func NewAllSessionsRevocationStore(
	pool *pgxpool.Pool,
) *AllSessionsRevocationStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &AllSessionsRevocationStore{
		pool: pool,
	}
}

func (s *AllSessionsRevocationStore) FindAllSessionRevocationTargetsByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	now time.Time,
) (auth.AllSessionsRevocationTarget, bool, error) {
	if refreshTokenHash == "" {
		return auth.AllSessionsRevocationTarget{},
			false,
			auth.ErrInvalidRefreshToken
	}

	if now.IsZero() {
		return auth.AllSessionsRevocationTarget{},
			false,
			errors.New(
				"all sessions revocation lookup time cannot be zero",
			)
	}

	now = now.UTC()

	const query = `
		SELECT
			source.identity_id::text,
			target_session.id::text,
			target_session.expires_at
		FROM (
			SELECT s.identity_id
			FROM refresh_tokens AS rt
			INNER JOIN auth_sessions AS s
				ON s.id = rt.session_id
			WHERE rt.token_hash = $1
			  AND rt.used_at IS NULL
			  AND rt.revoked_at IS NULL
			  AND rt.expires_at > $2
			  AND s.revoked_at IS NULL
			  AND s.expires_at > $2
		) AS source
		INNER JOIN auth_sessions AS target_session
			ON target_session.identity_id = source.identity_id
		WHERE target_session.revoked_at IS NULL
		ORDER BY target_session.id
	`

	rows, err := s.pool.Query(
		ctx,
		query,
		refreshTokenHash,
		now,
	)
	if err != nil {
		return auth.AllSessionsRevocationTarget{},
			false,
			fmt.Errorf(
				"find all session revocation targets: %w",
				err,
			)
	}
	defer rows.Close()

	target := auth.AllSessionsRevocationTarget{}

	for rows.Next() {
		var (
			identityID       string
			sessionID        string
			sessionExpiresAt time.Time
		)

		if err := rows.Scan(
			&identityID,
			&sessionID,
			&sessionExpiresAt,
		); err != nil {
			return auth.AllSessionsRevocationTarget{},
				false,
				fmt.Errorf(
					"scan all session revocation target: %w",
					err,
				)
		}

		if target.IdentityID == "" {
			target.IdentityID = identityID
		}

		target.Sessions = append(
			target.Sessions,
			auth.SessionRevocationTarget{
				SessionID:        sessionID,
				SessionExpiresAt: sessionExpiresAt.UTC(),
			},
		)
	}

	if err := rows.Err(); err != nil {
		return auth.AllSessionsRevocationTarget{},
			false,
			fmt.Errorf(
				"iterate all session revocation targets: %w",
				err,
			)
	}

	if len(target.Sessions) == 0 {
		return auth.AllSessionsRevocationTarget{},
			false,
			nil
	}

	return target, true, nil
}

func (s *AllSessionsRevocationStore) RevokeSessions(
	ctx context.Context,
	identityID string,
	sessionIDs []string,
	revokedAt time.Time,
) error {
	if identityID == "" {
		return errors.New(
			"identity ID cannot be empty",
		)
	}

	if len(sessionIDs) == 0 {
		return nil
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
			"begin exact sessions revocation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const revokeSessionsQuery = `
		UPDATE auth_sessions
		SET
			revoked_at = COALESCE(
				revoked_at,
				$1
			),
			updated_at = $1
		WHERE identity_id = $2::uuid
		  AND id::text = ANY($3::text[])
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeSessionsQuery,
		revokedAt,
		identityID,
		sessionIDs,
	); err != nil {
		return fmt.Errorf(
			"revoke exact authentication sessions: %w",
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
			  AND id::text = ANY($3::text[])
		)
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeRefreshTokensQuery,
		revokedAt,
		identityID,
		sessionIDs,
	); err != nil {
		return fmt.Errorf(
			"revoke exact session refresh tokens: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit exact sessions revocation: %w",
			err,
		)
	}

	return nil
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
