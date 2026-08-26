//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentifierUnlinkRequestStoreRejectsLastIdentifierRemoval(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000302",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-last-check@example.com",
	}

	const challengeID = "otp_ch_unlink_request_last_identifier"

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         "unlink-last-identifier-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, auth.ErrLastIdentifierRemoval) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			auth.ErrLastIdentifierRemoval,
		)
	}

	var challengeCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink OTP challenges: %v",
			err,
		)
	}

	if challengeCount != 0 {
		t.Fatalf(
			"rejected unlink challenge count = %d, want 0",
			challengeCount,
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink operations: %v",
			err,
		)
	}

	if operationCount != 0 {
		t.Fatalf(
			"rejected unlink operation count = %d, want 0",
			operationCount,
		)
	}
}

func TestIdentifierUnlinkRequestStoreRejectsTargetIdentifierNotLinked(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-target-missing-verification@example.com",
	}

	otherIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000303",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		otherIdentifier,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-target-missing@example.com",
	}

	const challengeID = "otp_ch_unlink_request_target_missing"

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         "unlink-target-missing-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, auth.ErrIdentifierNotLinked) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	var challengeCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink OTP challenges: %v",
			err,
		)
	}

	if challengeCount != 0 {
		t.Fatalf(
			"rejected unlink challenge count = %d, want 0",
			challengeCount,
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink operations: %v",
			err,
		)
	}

	if operationCount != 0 {
		t.Fatalf(
			"rejected unlink operation count = %d, want 0",
			operationCount,
		)
	}
}

func TestIdentifierUnlinkRequestStoreRejectsVerificationIdentifierNotLinked(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000304",
	}

	otherIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-verification-missing-other@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		otherIdentifier,
	)

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-verification-missing@example.com",
	}

	const challengeID = "otp_ch_unlink_request_verification_missing"

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         "unlink-verification-missing-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, auth.ErrIdentifierNotLinked) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	var challengeCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink OTP challenges: %v",
			err,
		)
	}

	if challengeCount != 0 {
		t.Fatalf(
			"rejected unlink challenge count = %d, want 0",
			challengeCount,
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink operations: %v",
			err,
		)
	}

	if operationCount != 0 {
		t.Fatalf(
			"rejected unlink operation count = %d, want 0",
			operationCount,
		)
	}
}
