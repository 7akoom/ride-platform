package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

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
	tenantHint, err := normalizeChallengeTenantHint(
		challenge.TenantHint,
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
		tenantHint,
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
		AND tenant_hint IS NOT DISTINCT FROM $4
		AND target_identity_id IS NULL
		AND verified_at IS NULL
		AND cancelled_at IS NULL
		AND expires_at > statement_timestamp()
	`

	cancelPreviousArgs := []any{
		string(identifier.Type),
		identifier.Value,
		string(purpose),
		tenantHint,
	}

	if targetIdentityID != nil {
		cancelPreviousQuery = `
			UPDATE otp_challenges
			SET cancelled_at = statement_timestamp()
			WHERE identifier_type = $1
			AND normalized_value = $2
			AND purpose = $3
			AND tenant_hint IS NOT DISTINCT FROM $4
			AND target_identity_id = $5::uuid
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
			tenant_hint,
			code_hash,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	if _, err := tx.Exec(
		ctx,
		insertQuery,
		challenge.ID,
		string(identifier.Type),
		identifier.Value,
		string(purpose),
		targetIdentityID,
		tenantHint,
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
