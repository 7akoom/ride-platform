package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type IssuedSession struct {
	SessionID      string
	RefreshTokenID string
}

type SessionStore struct {
	pool *pgxpool.Pool
}

func NewSessionStore(
	pool *pgxpool.Pool,
) *SessionStore {
	return &SessionStore{
		pool: pool,
	}
}

func (s *SessionStore) Create(
	ctx context.Context,
	sessionID string,
	identityID string,
	sessionExpiresAt time.Time,
	refreshTokenHash string,
	refreshTokenExpiresAt time.Time,
) (IssuedSession, error) {
	if sessionID == "" {
		return IssuedSession{}, errors.New("session ID cannot be empty")
	}

	if identityID == "" {
		return IssuedSession{}, errors.New("identity ID cannot be empty")
	}

	if refreshTokenHash == "" {
		return IssuedSession{}, errors.New("refresh token hash cannot be empty")
	}

	if refreshTokenExpiresAt.After(sessionExpiresAt) {
		return IssuedSession{}, errors.New(
			"refresh token cannot expire after its session",
		)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return IssuedSession{}, fmt.Errorf(
			"begin token issuance transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const sessionQuery = `
		INSERT INTO auth_sessions (
			id,
			identity_id,
			expires_at
		)
		VALUES ($1::uuid, $2::uuid, $3)
	`

	if _, err := tx.Exec(
		ctx,
		sessionQuery,
		sessionID,
		identityID,
		sessionExpiresAt,
	); err != nil {
		return IssuedSession{}, fmt.Errorf(
			"insert auth session: %w",
			err,
		)
	}

	const refreshTokenQuery = `
		INSERT INTO refresh_tokens (
			session_id,
			token_hash,
			expires_at
		)
		VALUES ($1::uuid, $2, $3)
		RETURNING id::text
	`

	var refreshTokenID string

	if err := tx.QueryRow(
		ctx,
		refreshTokenQuery,
		sessionID,
		refreshTokenHash,
		refreshTokenExpiresAt,
	).Scan(&refreshTokenID); err != nil {
		return IssuedSession{}, fmt.Errorf(
			"insert refresh token: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return IssuedSession{}, fmt.Errorf(
			"commit token issuance transaction: %w",
			err,
		)
	}

	return IssuedSession{
		SessionID:      sessionID,
		RefreshTokenID: refreshTokenID,
	}, nil
}