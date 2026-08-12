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

type ChallengeRepository struct {
	pool *pgxpool.Pool
}

func NewChallengeRepository(
	pool *pgxpool.Pool,
) *ChallengeRepository {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &ChallengeRepository{
		pool: pool,
	}
}

func (r *ChallengeRepository) Create(
	ctx context.Context,
	challenge auth.OTPChallenge,
) error {
	if strings.TrimSpace(challenge.ID) == "" {
		return errors.New(
			"OTP challenge ID cannot be blank",
		)
	}

	if strings.TrimSpace(challenge.PhoneNumber) == "" {
		return errors.New(
			"OTP challenge phone number cannot be blank",
		)
	}

	if strings.TrimSpace(challenge.CodeHash) == "" {
		return errors.New(
			"OTP challenge code hash cannot be blank",
		)
	}

	if challenge.ExpiresAt.IsZero() {
		return errors.New(
			"OTP challenge expiration cannot be zero",
		)
	}

	challenge.ExpiresAt = challenge.ExpiresAt.UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin OTP challenge creation transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	const lockQuery = `
		SELECT pg_advisory_xact_lock(
			hashtextextended($1, 0)
		)
	`

	if _, err := tx.Exec(
		ctx,
		lockQuery,
		challenge.PhoneNumber,
	); err != nil {
		return fmt.Errorf(
			"lock OTP challenges for phone number: %w",
			err,
		)
	}

	const cancelPreviousQuery = `
		UPDATE otp_challenges
		SET cancelled_at = statement_timestamp()
		WHERE phone_number = $1
		AND verified_at IS NULL
		AND cancelled_at IS NULL
		AND expires_at > statement_timestamp()
	`

	if _, err := tx.Exec(
		ctx,
		cancelPreviousQuery,
		challenge.PhoneNumber,
	); err != nil {
		return fmt.Errorf(
			"cancel previous OTP challenges: %w",
			err,
		)
	}

	const insertQuery = `
		INSERT INTO otp_challenges (
			id,
			phone_number,
			code_hash,
			expires_at
		)
		VALUES ($1, $2, $3, $4)
	`

	if _, err := tx.Exec(
		ctx,
		insertQuery,
		challenge.ID,
		challenge.PhoneNumber,
		challenge.CodeHash,
		challenge.ExpiresAt,
	); err != nil {
		return fmt.Errorf(
			"insert OTP challenge: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit OTP challenge creation transaction: %w",
			err,
		)
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

	return fmt.Errorf(
		"OTP challenge could not record failed attempt",
	)
}

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
