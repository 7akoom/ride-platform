package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

func (r *ChallengeRepository) MarkVerified(
	ctx context.Context,
	challengeID string,
	verifiedAt time.Time,
) error {
	if verifiedAt.IsZero() {
		return errors.New(
			"OTP verification time cannot be zero",
		)
	}

	verifiedAt = verifiedAt.UTC()

	const updateQuery = `
		UPDATE otp_challenges
		SET verified_at = $1
		WHERE id = $2
		  AND verified_at IS NULL
		  AND cancelled_at IS NULL
		  AND expires_at > $1
		  AND failed_attempts < max_attempts
		RETURNING id
	`

	var updatedChallengeID string

	err := r.pool.QueryRow(
		ctx,
		updateQuery,
		verifiedAt,
		challengeID,
	).Scan(
		&updatedChallengeID,
	)

	if err == nil {
		return nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(
			"mark OTP challenge verified: %w",
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
	var existingVerifiedAt *time.Time
	var cancelledAt *time.Time
	var failedAttempts int16
	var maxAttempts int16

	err = r.pool.QueryRow(
		ctx,
		stateQuery,
		challengeID,
	).Scan(
		&expiresAt,
		&existingVerifiedAt,
		&cancelledAt,
		&failedAttempts,
		&maxAttempts,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrChallengeNotFound
	}

	if err != nil {
		return fmt.Errorf(
			"query OTP challenge after verification failure: %w",
			err,
		)
	}

	if existingVerifiedAt != nil {
		return auth.ErrChallengeUsed
	}

	if cancelledAt != nil {
		return auth.ErrChallengeCancelled
	}

	if !verifiedAt.Before(expiresAt) {
		return auth.ErrChallengeExpired
	}

	if failedAttempts >= maxAttempts {
		return auth.ErrChallengeAttemptsExceeded
	}

	return errors.New(
		"OTP challenge could not be marked verified",
	)
}
