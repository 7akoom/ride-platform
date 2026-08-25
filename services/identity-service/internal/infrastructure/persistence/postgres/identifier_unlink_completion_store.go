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

type IdentifierUnlinkCompletionStore struct {
	pool *pgxpool.Pool
}

var _ auth.IdentifierUnlinkCompletionStore = (*IdentifierUnlinkCompletionStore)(nil)

func NewIdentifierUnlinkCompletionStore(
	pool *pgxpool.Pool,
) *IdentifierUnlinkCompletionStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentifierUnlinkCompletionStore{
		pool: pool,
	}
}

func (s *IdentifierUnlinkCompletionStore) Complete(
	ctx context.Context,
	input auth.IdentifierUnlinkCompletionInput,
) error {
	challengeID := strings.TrimSpace(input.ChallengeID)
	if challengeID == "" {
		return errors.New(
			"OTP challenge ID cannot be blank",
		)
	}

	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return errors.New(
			"identity ID cannot be blank",
		)
	}

	if input.VerifiedAt.IsZero() {
		return errors.New(
			"OTP verification time cannot be zero",
		)
	}

	verifiedAt := input.VerifiedAt.UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin identifier unlink completion transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	const identityQuery = `
		SELECT id::text
		FROM identities
		WHERE id = $1::uuid
		FOR UPDATE
	`

	var lockedIdentityID string

	err = tx.QueryRow(
		ctx,
		identityQuery,
		identityID,
	).Scan(
		&lockedIdentityID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrIdentityNotFound
	}

	if err != nil {
		return fmt.Errorf(
			"lock identity for identifier unlink: %w",
			err,
		)
	}

	const challengeQuery = `
		SELECT
			identifier_type,
			normalized_value,
			purpose,
			target_identity_id::text,
			expires_at,
			verified_at,
			cancelled_at,
			failed_attempts,
			max_attempts
		FROM otp_challenges
		WHERE id = $1
		FOR UPDATE
	`

	var storedVerificationIdentifierType string
	var storedVerificationNormalizedValue string
	var storedPurpose string
	var storedTargetIdentityID *string
	var expiresAt time.Time
	var existingVerifiedAt *time.Time
	var cancelledAt *time.Time
	var failedAttempts int16
	var maxAttempts int16

	err = tx.QueryRow(
		ctx,
		challengeQuery,
		challengeID,
	).Scan(
		&storedVerificationIdentifierType,
		&storedVerificationNormalizedValue,
		&storedPurpose,
		&storedTargetIdentityID,
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
			"query identifier unlink OTP challenge: %w",
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

	purpose, err := auth.ParseOTPPurpose(
		storedPurpose,
	)
	if err != nil {
		return fmt.Errorf(
			"restore OTP challenge purpose: %w",
			err,
		)
	}

	if purpose != auth.OTPPurposeUnlinkIdentifier {
		return errors.New(
			"OTP challenge is not for identifier unlinking",
		)
	}

	if storedTargetIdentityID == nil {
		return errors.New(
			"identifier unlink OTP challenge has no target identity",
		)
	}

	challengeIdentityID := strings.TrimSpace(
		*storedTargetIdentityID,
	)

	if challengeIdentityID == "" {
		return errors.New(
			"identifier unlink OTP challenge target identity is blank",
		)
	}

	if challengeIdentityID != identityID {
		return errors.New(
			"OTP challenge target identity does not match unlink request",
		)
	}

	verificationIdentifier, err := auth.NewIdentifier(
		auth.IdentifierType(
			storedVerificationIdentifierType,
		),
		storedVerificationNormalizedValue,
	)
	if err != nil {
		return fmt.Errorf(
			"restore identifier unlink verification identifier: %w",
			err,
		)
	}

	const operationQuery = `
		SELECT
			identity_id::text,
			identifier_type,
			normalized_value
		FROM identifier_unlink_operations
		WHERE challenge_id = $1
	`

	var operationIdentityID string
	var targetIdentifierType string
	var targetNormalizedValue string

	err = tx.QueryRow(
		ctx,
		operationQuery,
		challengeID,
	).Scan(
		&operationIdentityID,
		&targetIdentifierType,
		&targetNormalizedValue,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New(
			"identifier unlink operation was not found",
		)
	}

	if err != nil {
		return fmt.Errorf(
			"query identifier unlink operation: %w",
			err,
		)
	}

	if strings.TrimSpace(operationIdentityID) != identityID {
		return errors.New(
			"identifier unlink operation identity does not match challenge identity",
		)
	}

	targetIdentifier, err := auth.NewIdentifier(
		auth.IdentifierType(targetIdentifierType),
		targetNormalizedValue,
	)
	if err != nil {
		return fmt.Errorf(
			"restore identifier unlink target identifier: %w",
			err,
		)
	}

	if verificationIdentifier == targetIdentifier {
		return errors.New(
			"identifier unlink verification identifier cannot be the target identifier",
		)
	}

	if err := lockIdentityIdentifier(
		ctx,
		tx,
		targetIdentifier,
	); err != nil {
		return err
	}

	const verificationOwnershipQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		)
	`

	var verificationIdentifierOwned bool

	err = tx.QueryRow(
		ctx,
		verificationOwnershipQuery,
		identityID,
		string(verificationIdentifier.Type),
		verificationIdentifier.Value,
	).Scan(
		&verificationIdentifierOwned,
	)
	if err != nil {
		return fmt.Errorf(
			"check identifier unlink verification identifier ownership: %w",
			err,
		)
	}

	if !verificationIdentifierOwned {
		return auth.ErrIdentifierNotLinked
	}

	const targetOwnershipQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		)
	`

	var targetIdentifierOwned bool

	err = tx.QueryRow(
		ctx,
		targetOwnershipQuery,
		identityID,
		string(targetIdentifier.Type),
		targetIdentifier.Value,
	).Scan(
		&targetIdentifierOwned,
	)
	if err != nil {
		return fmt.Errorf(
			"check identifier unlink target ownership: %w",
			err,
		)
	}

	if !targetIdentifierOwned {
		return auth.ErrIdentifierNotLinked
	}

	const identifierCountQuery = `
		SELECT COUNT(*)
		FROM identity_identifiers
		WHERE identity_id = $1::uuid
	`

	var identifierCount int64

	err = tx.QueryRow(
		ctx,
		identifierCountQuery,
		identityID,
	).Scan(
		&identifierCount,
	)
	if err != nil {
		return fmt.Errorf(
			"count identity identifiers before unlink: %w",
			err,
		)
	}

	if identifierCount <= 1 {
		return auth.ErrLastIdentifierRemoval
	}

	const deleteIdentifierQuery = `
		DELETE FROM identity_identifiers
		WHERE identity_id = $1::uuid
		  AND identifier_type = $2
		  AND normalized_value = $3
		RETURNING id
	`

	var deletedIdentifierID string

	err = tx.QueryRow(
		ctx,
		deleteIdentifierQuery,
		identityID,
		string(targetIdentifier.Type),
		targetIdentifier.Value,
	).Scan(
		&deletedIdentifierID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrIdentifierNotLinked
	}

	if err != nil {
		return fmt.Errorf(
			"delete identity identifier: %w",
			err,
		)
	}

	const verifyChallengeQuery = `
		UPDATE otp_challenges
		SET verified_at = $1
		WHERE id = $2
		  AND verified_at IS NULL
		  AND cancelled_at IS NULL
		  AND expires_at > $1
		  AND failed_attempts < max_attempts
		RETURNING id
	`

	var verifiedChallengeID string

	err = tx.QueryRow(
		ctx,
		verifyChallengeQuery,
		verifiedAt,
		challengeID,
	).Scan(
		&verifiedChallengeID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New(
				"OTP challenge state changed during identifier unlinking",
			)
		}

		return fmt.Errorf(
			"mark identifier unlink OTP challenge verified: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit identifier unlink completion transaction: %w",
			err,
		)
	}

	return nil
}
