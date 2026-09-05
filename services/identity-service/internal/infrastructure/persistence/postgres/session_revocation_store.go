package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRevocationStore struct {
	pool *pgxpool.Pool
}

var _ auth.PersistentSessionRevocationStore = (*SessionRevocationStore)(nil)

var _ auth.SessionRevocationTargetStore = (*SessionRevocationStore)(nil)

var _ auth.SessionManagementRevocationTargetStore = (*SessionRevocationStore)(nil)

var _ auth.SessionAccessStateStore = (*SessionRevocationStore)(nil)

func NewSessionRevocationStore(
	pool *pgxpool.Pool,
) *SessionRevocationStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &SessionRevocationStore{
		pool: pool,
	}
}

func (s *SessionRevocationStore) FindSessionAccessState(
	ctx context.Context,
	sessionID string,
) (auth.SessionAccessState, bool, error) {
	if sessionID == "" {
		return auth.SessionAccessState{}, false, errors.New(
			"session ID cannot be empty",
		)
	}

	const query = `
		SELECT
			expires_at,
			revoked_at IS NOT NULL
		FROM auth_sessions
		WHERE id = $1
	`

	var state auth.SessionAccessState

	err := s.pool.QueryRow(
		ctx,
		query,
		sessionID,
	).Scan(
		&state.SessionExpiresAt,
		&state.Revoked,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionAccessState{}, false, nil
	}

	if err != nil {
		return auth.SessionAccessState{}, false, fmt.Errorf(
			"find session access state: %w",
			err,
		)
	}

	state.SessionExpiresAt =
		state.SessionExpiresAt.UTC()

	return state, true, nil
}

func (s *SessionRevocationStore) FindRevocationTargetByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
) (auth.SessionRevocationTarget, bool, error) {
	if refreshTokenHash == "" {
		return auth.SessionRevocationTarget{}, false, auth.ErrInvalidRefreshToken
	}

	const query = `
		SELECT
			s.id::text,
			s.expires_at
		FROM refresh_tokens AS rt
		INNER JOIN auth_sessions AS s
			ON s.id = rt.session_id
		WHERE rt.token_hash = $1
	`

	var target auth.SessionRevocationTarget

	err := s.pool.QueryRow(
		ctx,
		query,
		refreshTokenHash,
	).Scan(
		&target.SessionID,
		&target.SessionExpiresAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionRevocationTarget{}, false, nil
	}

	if err != nil {
		return auth.SessionRevocationTarget{}, false, fmt.Errorf(
			"find session revocation target by refresh token hash: %w",
			err,
		)
	}

	target.SessionExpiresAt = target.SessionExpiresAt.UTC()

	return target, true, nil
}

func (s *SessionRevocationStore) FindRevocationTargetByIdentityAndSessionID(
	ctx context.Context,
	identityID string,
	sessionID string,
) (auth.SessionRevocationTarget, bool, error) {
	if strings.TrimSpace(identityID) == "" {
		return auth.SessionRevocationTarget{}, false, errors.New(
			"identity ID cannot be blank",
		)
	}

	if strings.TrimSpace(sessionID) == "" {
		return auth.SessionRevocationTarget{}, false, errors.New(
			"session ID cannot be blank",
		)
	}

	const query = `
		SELECT
			id::text,
			expires_at
		FROM auth_sessions
		WHERE identity_id = $1::uuid
		  AND id::text = $2
	`

	var target auth.SessionRevocationTarget

	err := s.pool.QueryRow(
		ctx,
		query,
		identityID,
		sessionID,
	).Scan(
		&target.SessionID,
		&target.SessionExpiresAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionRevocationTarget{}, false, nil
	}

	if err != nil {
		return auth.SessionRevocationTarget{}, false, fmt.Errorf(
			"find session revocation target by identity and session ID: %w",
			err,
		)
	}

	target.SessionExpiresAt =
		target.SessionExpiresAt.UTC()

	return target, true, nil
}

func (s *SessionRevocationStore) RevokeSessionByRefreshTokenHash(
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
		SELECT
			i.id::text,
			s.id::text,
			s.revoked_at
		FROM refresh_tokens AS rt
		INNER JOIN auth_sessions AS s
			ON s.id = rt.session_id
		INNER JOIN identities AS i
			ON i.id = s.identity_id
		WHERE rt.token_hash = $1
		FOR UPDATE OF rt, s
	`

	var (
		identityID       string
		sessionID        string
		sessionRevokedAt *time.Time
	)

	err = tx.QueryRow(
		ctx,
		selectSessionQuery,
		refreshTokenHash,
	).Scan(
		&identityID,
		&sessionID,
		&sessionRevokedAt,
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

	if sessionRevokedAt == nil {
		event, err :=
			auth.NewIdentitySessionRevokedDomainEvent(
				identityID,
				sessionID,
				revokedAt,
			)
		if err != nil {
			return fmt.Errorf(
				"build identity session revoked domain event: %w",
				err,
			)
		}

		if err :=
			insertIdentitySessionRevokedOutboxEventInTransaction(
				ctx,
				tx,
				event,
			); err != nil {
			return fmt.Errorf(
				"persist identity session revoked domain event: %w",
				err,
			)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit session revocation: %w",
			err,
		)
	}

	return nil
}
