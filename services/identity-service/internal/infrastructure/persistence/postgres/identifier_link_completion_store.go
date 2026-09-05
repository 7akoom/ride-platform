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

type IdentifierLinkCompletionStore struct {
	pool *pgxpool.Pool
}

var _ auth.IdentifierLinkCompletionStore = (*IdentifierLinkCompletionStore)(nil)

func NewIdentifierLinkCompletionStore(
	pool *pgxpool.Pool,
) *IdentifierLinkCompletionStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentifierLinkCompletionStore{
		pool: pool,
	}
}

func (s *IdentifierLinkCompletionStore) Complete(
	ctx context.Context,
	input auth.IdentifierLinkCompletionInput,
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

	identifier, err := auth.NewIdentifier(
		input.Identifier.Type,
		input.Identifier.Value,
	)
	if err != nil {
		return err
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
			"begin identifier link completion transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

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

	var storedIdentifierType string
	var storedNormalizedValue string
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
		&storedIdentifierType,
		&storedNormalizedValue,
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
			"query identifier link OTP challenge: %w",
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

	storedIdentifier, err := auth.NewIdentifier(
		auth.IdentifierType(storedIdentifierType),
		storedNormalizedValue,
	)
	if err != nil {
		return fmt.Errorf(
			"restore OTP challenge identifier: %w",
			err,
		)
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

	if purpose != auth.OTPPurposeLinkIdentifier {
		return errors.New(
			"OTP challenge is not for identifier linking",
		)
	}

	if storedTargetIdentityID == nil {
		return errors.New(
			"identifier link OTP challenge has no target identity",
		)
	}

	if *storedTargetIdentityID != identityID {
		return errors.New(
			"OTP challenge target identity does not match link request",
		)
	}

	if storedIdentifier != identifier {
		return errors.New(
			"OTP challenge identifier does not match link request",
		)
	}

	if err := lockIdentityIdentifier(
		ctx,
		tx,
		identifier,
	); err != nil {
		return err
	}

	const existingOwnerQuery = `
		SELECT identity_id::text
		FROM identity_identifiers
		WHERE identifier_type = $1
		  AND normalized_value = $2
	`

	var existingIdentityID string
	identifierLinked := false

	err = tx.QueryRow(
		ctx,
		existingOwnerQuery,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&existingIdentityID,
	)

	switch {
	case err == nil:
		if existingIdentityID != identityID {
			return auth.ErrIdentifierAlreadyLinked
		}

	case errors.Is(err, pgx.ErrNoRows):
		const insertIdentifierQuery = `
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1, $2, $3, $4)
		`

		if _, err := tx.Exec(
			ctx,
			insertIdentifierQuery,
			identityID,
			string(identifier.Type),
			identifier.Value,
			verifiedAt,
		); err != nil {
			if isIdentifierOwnershipConflict(err) {
				return auth.ErrIdentifierAlreadyLinked
			}

			return fmt.Errorf(
				"insert linked identity identifier: %w",
				err,
			)
		}

		identifierLinked = true

	default:
		return fmt.Errorf(
			"query identifier ownership: %w",
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
				"OTP challenge state changed during identifier linking",
			)
		}

		return fmt.Errorf(
			"mark identifier link OTP challenge verified: %w",
			err,
		)
	}

	if identifierLinked {
		event, err := auth.NewIdentityIdentifierLinkedDomainEvent(
			identityID,
			identifier.Type,
			verifiedAt,
		)
		if err != nil {
			return fmt.Errorf(
				"build identity identifier linked domain event: %w",
				err,
			)
		}

		if err := insertIdentityIdentifierOutboxEventInTransaction(
			ctx,
			tx,
			event,
		); err != nil {
			return fmt.Errorf(
				"persist identity identifier linked domain event: %w",
				err,
			)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit identifier link completion transaction: %w",
			err,
		)
	}

	return nil
}
