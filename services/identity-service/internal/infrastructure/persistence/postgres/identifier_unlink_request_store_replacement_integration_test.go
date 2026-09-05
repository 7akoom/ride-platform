//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentifierUnlinkRequestStoreCancelsPreviousActiveChallenge(
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
		Value: "+9647500000305",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-request-replacement@example.com",
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
		verificationIdentifier,
	)

	const firstChallengeID = "otp_ch_unlink_request_replacement_first"
	const secondChallengeID = "otp_ch_unlink_request_replacement_second"

	firstChallenge := auth.OTPChallenge{
		ID:               firstChallengeID,
		Identifier:       verificationIdentifier,
		Purpose:          auth.OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "unlink-request-replacement-first-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge:        firstChallenge,
			TargetIdentifier: targetIdentifier,
		},
	); err != nil {
		t.Fatalf(
			"create first unlink request: %v",
			err,
		)
	}

	secondChallenge := auth.OTPChallenge{
		ID:               secondChallengeID,
		Identifier:       verificationIdentifier,
		Purpose:          auth.OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "unlink-request-replacement-second-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge:        secondChallenge,
			TargetIdentifier: targetIdentifier,
		},
	); err != nil {
		t.Fatalf(
			"create replacement unlink request: %v",
			err,
		)
	}

	var firstCancelledAt *time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT cancelled_at
			FROM otp_challenges
			WHERE id = $1
		`,
		firstChallengeID,
	).Scan(
		&firstCancelledAt,
	); err != nil {
		t.Fatalf(
			"query first unlink challenge: %v",
			err,
		)
	}

	if firstCancelledAt == nil {
		t.Fatal(
			"first unlink challenge was not cancelled",
		)
	}

	var secondCancelledAt *time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT cancelled_at
			FROM otp_challenges
			WHERE id = $1
		`,
		secondChallengeID,
	).Scan(
		&secondCancelledAt,
	); err != nil {
		t.Fatalf(
			"query replacement unlink challenge: %v",
			err,
		)
	}

	if secondCancelledAt != nil {
		t.Fatal(
			"replacement unlink challenge was unexpectedly cancelled",
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id IN ($1, $2)
		`,
		firstChallengeID,
		secondChallengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count replacement unlink operations: %v",
			err,
		)
	}

	if operationCount != 2 {
		t.Fatalf(
			"unlink operation count = %d, want 2",
			operationCount,
		)
	}
}
