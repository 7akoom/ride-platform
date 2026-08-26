//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestChallengeRepositoryScopesLinkIdentifierLatestChallengeByTargetIdentity(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const firstTargetChallengeID = "otp_ch_link_target_a_first"
	const secondTargetChallengeID = "otp_ch_link_target_a_second"
	const otherTargetChallengeID = "otp_ch_link_target_b"

	firstIdentityID := createIntegrationIdentity(
		t,
		ctx,
		pool,
	)

	secondIdentityID := createIntegrationIdentity(
		t,
		ctx,
		pool,
	)

	cleanupChallengeIDs(
		t,
		pool,
		firstTargetChallengeID,
		secondTargetChallengeID,
		otherTargetChallengeID,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "scope@example.com",
	}

	now := time.Now().UTC()

	firstTargetChallenge := auth.OTPChallenge{
		ID:               firstTargetChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &firstIdentityID,
		CodeHash:         "target-a-first-code-hash",
		ExpiresAt:        now.Add(5 * time.Minute),
	}

	otherTargetChallenge := auth.OTPChallenge{
		ID:               otherTargetChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &secondIdentityID,
		CodeHash:         "target-b-code-hash",
		ExpiresAt:        now.Add(5 * time.Minute),
	}

	secondTargetChallenge := auth.OTPChallenge{
		ID:               secondTargetChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &firstIdentityID,
		CodeHash:         "target-a-second-code-hash",
		ExpiresAt:        now.Add(5 * time.Minute),
	}

	for _, challenge := range []auth.OTPChallenge{
		firstTargetChallenge,
		otherTargetChallenge,
		secondTargetChallenge,
	} {
		if err := repository.Create(
			ctx,
			challenge,
		); err != nil {
			t.Fatalf(
				"Create(%q) returned an error: %v",
				challenge.ID,
				err,
			)
		}
	}

	firstStored, err := repository.FindByID(
		ctx,
		firstTargetChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"find first target challenge: %v",
			err,
		)
	}

	secondStored, err := repository.FindByID(
		ctx,
		secondTargetChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"find second target challenge: %v",
			err,
		)
	}

	otherStored, err := repository.FindByID(
		ctx,
		otherTargetChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"find other target challenge: %v",
			err,
		)
	}

	if firstStored.CancelledAt == nil {
		t.Fatal(
			"older challenge for same target identity remained active",
		)
	}

	if secondStored.CancelledAt != nil {
		t.Fatal(
			"latest challenge for first target identity was cancelled",
		)
	}

	if otherStored.CancelledAt != nil {
		t.Fatal(
			"challenge for different target identity was incorrectly cancelled",
		)
	}
}
