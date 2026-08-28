package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdentifierUnlinkRequestStore struct {
	pool *pgxpool.Pool
}

var _ auth.IdentifierUnlinkRequestStore = (*IdentifierUnlinkRequestStore)(nil)

func NewIdentifierUnlinkRequestStore(
	pool *pgxpool.Pool,
) *IdentifierUnlinkRequestStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentifierUnlinkRequestStore{
		pool: pool,
	}
}

func (s *IdentifierUnlinkRequestStore) Create(
	ctx context.Context,
	input auth.IdentifierUnlinkRequestInput,
) error {
	challenge := input.Challenge

	challengeID := strings.TrimSpace(challenge.ID)
	if challengeID == "" {
		return errors.New(
			"OTP challenge ID cannot be blank",
		)
	}

	verificationIdentifier, err := auth.NewIdentifier(
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

	if purpose != auth.OTPPurposeUnlinkIdentifier {
		return errors.New(
			"OTP challenge is not for identifier unlinking",
		)
	}

	if challenge.TargetIdentityID == nil {
		return errors.New(
			"identifier unlink OTP challenge requires target identity",
		)
	}

	identityID := strings.TrimSpace(
		*challenge.TargetIdentityID,
	)
	if identityID == "" {
		return errors.New(
			"OTP challenge target identity cannot be blank",
		)
	}

	targetIdentifier, err := auth.NewIdentifier(
		input.TargetIdentifier.Type,
		input.TargetIdentifier.Value,
	)
	if err != nil {
		return err
	}

	if verificationIdentifier == targetIdentifier {
		return errors.New(
			"verification identifier cannot be the identifier being unlinked",
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

	expiresAt := challenge.ExpiresAt.UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin identifier unlink request transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	const lockIdentityQuery = `
		SELECT id::text
		FROM identities
		WHERE id = $1::uuid
		FOR UPDATE
	`

	var lockedIdentityID string

	err = tx.QueryRow(
		ctx,
		lockIdentityQuery,
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

	const targetOwnershipQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		)
	`

	var targetOwned bool

	if err := tx.QueryRow(
		ctx,
		targetOwnershipQuery,
		identityID,
		string(targetIdentifier.Type),
		targetIdentifier.Value,
	).Scan(
		&targetOwned,
	); err != nil {
		return fmt.Errorf(
			"check identifier unlink target ownership: %w",
			err,
		)
	}

	if !targetOwned {
		return auth.ErrIdentifierNotLinked
	}

	const identifierCountQuery = `
	SELECT COUNT(*)
	FROM identity_identifiers
	WHERE identity_id = $1::uuid
`

	var identifierCount int

	if err := tx.QueryRow(
		ctx,
		identifierCountQuery,
		identityID,
	).Scan(
		&identifierCount,
	); err != nil {
		return fmt.Errorf(
			"count identity identifiers before unlink request: %w",
			err,
		)
	}

	if identifierCount <= 1 {
		return auth.ErrLastIdentifierRemoval
	}

	var verificationOwned bool

	if err := tx.QueryRow(
		ctx,
		targetOwnershipQuery,
		identityID,
		string(verificationIdentifier.Type),
		verificationIdentifier.Value,
	).Scan(
		&verificationOwned,
	); err != nil {
		return fmt.Errorf(
			"check unlink verification identifier ownership: %w",
			err,
		)
	}

	if !verificationOwned {
		return auth.ErrIdentifierNotLinked
	}

	tenantHint, err := normalizeChallengeTenantHint(
		challenge.TenantHint,
	)
	if err != nil {
		return err
	}

	const cancelPreviousQuery = `
		UPDATE otp_challenges
		SET cancelled_at = statement_timestamp()
		WHERE identifier_type = $1
		AND normalized_value = $2
		AND purpose = $3
		AND target_identity_id = $4::uuid
		AND tenant_hint IS NOT DISTINCT FROM $5
		AND verified_at IS NULL
		AND cancelled_at IS NULL
		AND expires_at > statement_timestamp()
	`

	if _, err := tx.Exec(
		ctx,
		cancelPreviousQuery,
		string(verificationIdentifier.Type),
		verificationIdentifier.Value,
		string(purpose),
		identityID,
		tenantHint,
	); err != nil {
		return fmt.Errorf(
			"cancel previous identifier unlink OTP challenges: %w",
			err,
		)
	}

	const insertChallengeQuery = `
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
		VALUES ($1, $2, $3, $4, $5::uuid, $6, $7, $8)
	`

	if _, err := tx.Exec(
		ctx,
		insertChallengeQuery,
		challengeID,
		string(verificationIdentifier.Type),
		verificationIdentifier.Value,
		string(purpose),
		identityID,
		tenantHint,
		challenge.CodeHash,
		expiresAt,
	); err != nil {
		return fmt.Errorf(
			"insert identifier unlink OTP challenge: %w",
			err,
		)
	}

	const insertOperationQuery = `
		INSERT INTO identifier_unlink_operations (
			challenge_id,
			identity_id,
			identifier_type,
			normalized_value
		)
		VALUES ($1, $2::uuid, $3, $4)
	`

	if _, err := tx.Exec(
		ctx,
		insertOperationQuery,
		challengeID,
		identityID,
		string(targetIdentifier.Type),
		targetIdentifier.Value,
	); err != nil {
		return fmt.Errorf(
			"insert identifier unlink operation: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit identifier unlink request transaction: %w",
			err,
		)
	}

	return nil
}
