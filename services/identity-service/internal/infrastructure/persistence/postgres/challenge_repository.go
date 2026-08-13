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

var _ auth.ChallengeRepository = (*ChallengeRepository)(nil)

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

	identifier, err := auth.NewIdentifier(
		challenge.Identifier.Type,
		challenge.Identifier.Value,
	)
	if err != nil {
		return err
	}

	purpose, err := auth.ParseOTPPurpose(
		string(challenge.Purpose),
	)
	if err != nil {
		return err
	}

	targetIdentityID, err := normalizeChallengeTargetIdentityID(
		purpose,
		challenge.TargetIdentityID,
	)
	if err != nil {
		return err
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

	lockKey := challengeScopeLockKey(
		identifier,
		purpose,
		targetIdentityID,
	)

	const lockQuery = `
		SELECT pg_advisory_xact_lock(
			hashtextextended($1, 0)
		)
	`

	if _, err := tx.Exec(
		ctx,
		lockQuery,
		lockKey,
	); err != nil {
		return fmt.Errorf(
			"lock OTP challenge scope: %w",
			err,
		)
	}

	cancelPreviousQuery := `
		UPDATE otp_challenges
		SET cancelled_at = statement_timestamp()
		WHERE identifier_type = $1
		AND normalized_value = $2
		AND purpose = $3
		AND target_identity_id IS NULL
		AND verified_at IS NULL
		AND cancelled_at IS NULL
		AND expires_at > statement_timestamp()
	`

	cancelPreviousArgs := []any{
		string(identifier.Type),
		identifier.Value,
		string(purpose),
	}

	if targetIdentityID != nil {
		cancelPreviousQuery = `
			UPDATE otp_challenges
			SET cancelled_at = statement_timestamp()
			WHERE identifier_type = $1
			AND normalized_value = $2
			AND purpose = $3
			AND target_identity_id = $4::uuid
			AND verified_at IS NULL
			AND cancelled_at IS NULL
			AND expires_at > statement_timestamp()
		`

		cancelPreviousArgs = append(
			cancelPreviousArgs,
			targetIdentityID,
		)
	}

	if _, err := tx.Exec(
		ctx,
		cancelPreviousQuery,
		cancelPreviousArgs...,
	); err != nil {
		return fmt.Errorf(
			"cancel previous OTP challenges: %w",
			err,
		)
	}

	const insertQuery = `
		INSERT INTO otp_challenges (
			id,
			identifier_type,
			normalized_value,
			purpose,
			target_identity_id,
			code_hash,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	if _, err := tx.Exec(
		ctx,
		insertQuery,
		challenge.ID,
		string(identifier.Type),
		identifier.Value,
		string(purpose),
		targetIdentityID,
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
			identifier_type,
			normalized_value,
			purpose,
			target_identity_id::text,
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
	var identifierType string
	var normalizedValue string
	var purpose string
	var targetIdentityID *string

	err := r.pool.QueryRow(
		ctx,
		query,
		challengeID,
	).Scan(
		&challenge.ID,
		&identifierType,
		&normalizedValue,
		&purpose,
		&targetIdentityID,
		&challenge.CodeHash,
		&challenge.ExpiresAt,
		&challenge.VerifiedAt,
		&challenge.CancelledAt,
		&challenge.FailedAttempts,
		&challenge.MaxAttempts,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.OTPChallenge{},
			auth.ErrChallengeNotFound
	}

	if err != nil {
		return auth.OTPChallenge{}, fmt.Errorf(
			"query OTP challenge: %w",
			err,
		)
	}

	identifier, err := auth.NewIdentifier(
		auth.IdentifierType(identifierType),
		normalizedValue,
	)
	if err != nil {
		return auth.OTPChallenge{}, fmt.Errorf(
			"restore OTP challenge identifier: %w",
			err,
		)
	}

	parsedPurpose, err := auth.ParseOTPPurpose(
		purpose,
	)
	if err != nil {
		return auth.OTPChallenge{}, fmt.Errorf(
			"restore OTP challenge purpose: %w",
			err,
		)
	}

	if _, err := normalizeChallengeTargetIdentityID(
		parsedPurpose,
		targetIdentityID,
	); err != nil {
		return auth.OTPChallenge{}, fmt.Errorf(
			"restore OTP challenge target identity: %w",
			err,
		)
	}

	challenge.Identifier = identifier
	challenge.Purpose = parsedPurpose
	challenge.TargetIdentityID = targetIdentityID

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

	return errors.New(
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

func normalizeChallengeTargetIdentityID(
	purpose auth.OTPPurpose,
	targetIdentityID *string,
) (any, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		if targetIdentityID != nil {
			return nil, errors.New(
				"login OTP challenge cannot target an identity",
			)
		}

		return nil, nil

	case auth.OTPPurposeLinkIdentifier:
		if targetIdentityID == nil {
			return nil, errors.New(
				"link identifier OTP challenge requires target identity",
			)
		}

		normalized := strings.TrimSpace(
			*targetIdentityID,
		)
		if normalized == "" {
			return nil, errors.New(
				"OTP challenge target identity cannot be blank",
			)
		}

		return normalized, nil

	default:
		return nil, auth.ErrInvalidOTPPurpose
	}
}

func challengeScopeLockKey(
	identifier auth.Identifier,
	purpose auth.OTPPurpose,
	targetIdentityID any,
) string {
	target := "-"

	if targetIdentityID != nil {
		target = fmt.Sprint(targetIdentityID)
	}

	return string(identifier.Type) +
		":" +
		identifier.Value +
		":" +
		string(purpose) +
		":" +
		target
}
