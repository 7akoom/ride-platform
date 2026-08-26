package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

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
