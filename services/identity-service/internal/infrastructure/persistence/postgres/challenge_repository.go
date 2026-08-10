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

type ChallengeRepository struct {
	pool *pgxpool.Pool
}

func NewChallengeRepository(
	pool *pgxpool.Pool,
) *ChallengeRepository {
	return &ChallengeRepository{
		pool: pool,
	}
}

func (r *ChallengeRepository) Create(
	ctx context.Context,
	challenge auth.OTPChallenge,
) error {
	const query = `
		INSERT INTO otp_challenges (
			id,
			phone_number,
			code_hash,
			expires_at
		)
		VALUES ($1, $2, $3, $4)
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		challenge.ID,
		challenge.PhoneNumber,
		challenge.CodeHash,
		challenge.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert OTP challenge: %w", err)
	}

	return nil
}

func (r *ChallengeRepository) FindByID(
	ctx context.Context,
	challengeID string,
) (auth.OTPChallenge, error) {
	const query = `
		SELECT
			id,
			phone_number,
			code_hash,
			expires_at,
			verified_at,
			cancelled_at,
			failed_attempts,
			max_attempts
		FROM otp_challenges
		WHERE id = $1
	`

	var challenge auth.OTPChallenge

	err := r.pool.QueryRow(
		ctx,
		query,
		challengeID,
	).Scan(
		&challenge.ID,
		&challenge.PhoneNumber,
		&challenge.CodeHash,
		&challenge.ExpiresAt,
		&challenge.VerifiedAt,
		&challenge.CancelledAt,
		&challenge.FailedAttempts,
		&challenge.MaxAttempts,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.OTPChallenge{}, auth.ErrChallengeNotFound
	}

	if err != nil {
		return auth.OTPChallenge{}, fmt.Errorf(
			"query OTP challenge: %w",
			err,
		)
	}

	return challenge, nil
}

func (r *ChallengeRepository) RecordFailedAttempt(
	ctx context.Context,
	challengeID string,
	attemptedAt time.Time,
) error {
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

	return fmt.Errorf(
		"OTP challenge could not record failed attempt",
	)
}

func (r *ChallengeRepository) MarkVerified(
	ctx context.Context,
	challengeID string,
	verifiedAt time.Time,
) error {
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
	var failedAttempts int16
	var maxAttempts int16
	var cancelledAt *time.Time

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

	return fmt.Errorf(
		"OTP challenge could not be marked verified",
	)
}

func (r *ChallengeRepository) Cancel(
	ctx context.Context,
	challengeID string,
	cancelledAt time.Time,
) error {
	const updateQuery = `
		UPDATE otp_challenges
		SET cancelled_at = $1
		WHERE id = $2
		  AND verified_at IS NULL
		  AND cancelled_at IS NULL
		  AND expires_at > $1
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

	if !cancelledAt.Before(expiresAt) {
		return auth.ErrChallengeExpired
	}

	return fmt.Errorf(
		"OTP challenge could not be cancelled",
	)
}