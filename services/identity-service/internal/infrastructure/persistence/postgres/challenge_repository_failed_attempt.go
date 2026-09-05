package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

func (r *ChallengeRepository) RecordFailedAttempt(
	ctx context.Context,
	challengeID string,
	attemptedAt time.Time,
) error {
	if attemptedAt.IsZero() {
		return errors.New(
			"OTP failed attempt time cannot be zero",
		)
	}

	attemptedAt = attemptedAt.UTC()

	const updateQuery = `
		UPDATE otp_challenges
		SET failed_attempts = failed_attempts + 1
		WHERE id = $1
		  AND verified_at IS NULL
		  AND cancelled_at IS NULL
		  AND expires_at > $2
		  AND failed_attempts < max_attempts
		RETURNING
			failed_attempts,
			max_attempts
	`

	var failedAttempts int16
	var maxAttempts int16

	err := r.pool.QueryRow(
		ctx,
		updateQuery,
		challengeID,
		attemptedAt,
	).Scan(
		&failedAttempts,
		&maxAttempts,
	)

	if err == nil {
		if failedAttempts >= maxAttempts {
			return auth.ErrChallengeAttemptsExceeded
		}

		return nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(
			"record failed OTP attempt: %w",
			err,
		)
	}

	const stateQuery = `
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
	var verifiedAt *time.Time
	var cancelledAt *time.Time

	err = r.pool.QueryRow(
		ctx,
		stateQuery,
		challengeID,
	).Scan(
		&expiresAt,
		&verifiedAt,
		&cancelledAt,
		&failedAttempts,
		&maxAttempts,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrChallengeNotFound
	}

	if err != nil {
		return fmt.Errorf(
			"query OTP challenge after failed attempt: %w",
			err,
		)
	}

	if verifiedAt != nil {
		return auth.ErrChallengeUsed
	}

	if cancelledAt != nil {
		return auth.ErrChallengeCancelled
	}

	if !attemptedAt.Before(expiresAt) {
		return auth.ErrChallengeExpired
	}

	if failedAttempts >= maxAttempts {
		return auth.ErrChallengeAttemptsExceeded
	}

	return errors.New(
		"OTP challenge could not record failed attempt",
	)
}
