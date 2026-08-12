package token

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
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &SessionStore{
		pool: pool,
	}
}

func (s *SessionStore) Create(
	ctx context.Context,
	input SessionCreationInput,
) (IssuedSession, error) {
	if strings.TrimSpace(input.ChallengeID) == "" {
		return IssuedSession{}, errors.New(
			"challenge ID cannot be blank",
		)
	}

	if input.VerifiedAt.IsZero() {
		return IssuedSession{}, errors.New(
			"OTP verification time cannot be zero",
		)
	}

	if input.SessionID == "" {
		return IssuedSession{}, errors.New(
			"session ID cannot be empty",
		)
	}

	if input.IdentityID == "" {
		return IssuedSession{}, errors.New(
			"identity ID cannot be empty",
		)
	}

	if input.RefreshTokenHash == "" {
		return IssuedSession{}, errors.New(
			"refresh token hash cannot be empty",
		)
	}

	if input.SessionExpiresAt.IsZero() {
		return IssuedSession{}, errors.New(
			"session expiration cannot be zero",
		)
	}

	if input.RefreshTokenExpiresAt.IsZero() {
		return IssuedSession{}, errors.New(
			"refresh token expiration cannot be zero",
		)
	}

	input.VerifiedAt = input.VerifiedAt.UTC()
	input.SessionExpiresAt = input.SessionExpiresAt.UTC()
	input.RefreshTokenExpiresAt =
		input.RefreshTokenExpiresAt.UTC()

	if input.RefreshTokenExpiresAt.After(
		input.SessionExpiresAt,
	) {
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

	const verifyChallengeQuery = `
		UPDATE otp_challenges
		SET verified_at = $1
		WHERE id = $2
		  AND verified_at IS NULL
		  AND cancelled_at IS NULL
		  AND expires_at > $1
		  AND failed_attempts < max_attempts
		RETURNING id::text
	`

	var verifiedChallengeID string

	err = tx.QueryRow(
		ctx,
		verifyChallengeQuery,
		input.VerifiedAt,
		input.ChallengeID,
	).Scan(
		&verifiedChallengeID,
	)

	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return IssuedSession{}, fmt.Errorf(
				"mark OTP challenge verified: %w",
				err,
			)
		}

		const challengeStateQuery = `
			SELECT
				expires_at,
				verified_at,
				cancelled_at,
				failed_attempts,
				max_attempts
			FROM otp_challenges
			WHERE id = $1
		`

		var expiresAt time.Time
		var existingVerifiedAt *time.Time
		var cancelledAt *time.Time
		var failedAttempts int16
		var maxAttempts int16

		stateErr := tx.QueryRow(
			ctx,
			challengeStateQuery,
			input.ChallengeID,
		).Scan(
			&expiresAt,
			&existingVerifiedAt,
			&cancelledAt,
			&failedAttempts,
			&maxAttempts,
		)

		if errors.Is(stateErr, pgx.ErrNoRows) {
			return IssuedSession{},
				auth.ErrChallengeNotFound
		}

		if stateErr != nil {
			return IssuedSession{}, fmt.Errorf(
				"query OTP challenge after verification failure: %w",
				stateErr,
			)
		}

		if existingVerifiedAt != nil {
			return IssuedSession{},
				auth.ErrChallengeUsed
		}

		if cancelledAt != nil {
			return IssuedSession{},
				auth.ErrChallengeCancelled
		}

		if !input.VerifiedAt.Before(expiresAt) {
			return IssuedSession{},
				auth.ErrChallengeExpired
		}

		if failedAttempts >= maxAttempts {
			return IssuedSession{},
				auth.ErrChallengeAttemptsExceeded
		}

		return IssuedSession{}, errors.New(
			"OTP challenge could not be marked verified",
		)
	}

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
		input.SessionID,
		input.IdentityID,
		input.SessionExpiresAt,
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
		input.SessionID,
		input.RefreshTokenHash,
		input.RefreshTokenExpiresAt,
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
		SessionID:      input.SessionID,
		RefreshTokenID: refreshTokenID,
	}, nil
}
