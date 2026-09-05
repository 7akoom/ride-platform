package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

func (r *ChallengeRepository) Cancel(
	ctx context.Context,
	challengeID string,
	cancelledAt time.Time,
) error {
	if cancelledAt.IsZero() {
		return errors.New(
			"OTP cancellation time cannot be zero",
		)
	}

	cancelledAt = cancelledAt.UTC()

	const updateQuery = `
		UPDATE otp_challenges
		SET cancelled_at = $1
		WHERE id = $2
		  AND verified_at IS NULL
		  AND cancelled_at IS NULL
		  AND expires_at >= $1
		RETURNING id
	`

	var updatedChallengeID string

	err := r.pool.QueryRow(
		ctx,
		updateQuery,
		cancelledAt,
		challengeID,
	).Scan(
		&updatedChallengeID,
	)

	if err == nil {
		return nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(
			"cancel OTP challenge: %w",
			err,
		)
	}

	const stateQuery = `
		SELECT
			expires_at,
			verified_at,
			cancelled_at
		FROM otp_challenges
		WHERE id = $1
	`

	var expiresAt time.Time
	var verifiedAt *time.Time
	var existingCancelledAt *time.Time

	err = r.pool.QueryRow(
		ctx,
		stateQuery,
		challengeID,
	).Scan(
		&expiresAt,
		&verifiedAt,
		&existingCancelledAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrChallengeNotFound
	}

	if err != nil {
		return fmt.Errorf(
			"query OTP challenge after cancellation failure: %w",
			err,
		)
	}

	if verifiedAt != nil {
		return auth.ErrChallengeUsed
	}

	if existingCancelledAt != nil {
		return nil
	}

	if cancelledAt.After(expiresAt) {
		return auth.ErrChallengeExpired
	}

	return errors.New(
		"OTP challenge could not be cancelled",
	)
}
