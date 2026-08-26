//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createIdentifierUnlinkCompletionRequest(
	t *testing.T,
	ctx context.Context,
	requestStore *IdentifierUnlinkRequestStore,
	identityID string,
	challengeID string,
	targetIdentifier auth.Identifier,
	verificationIdentifier auth.Identifier,
) {
	t.Helper()

	err := requestStore.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         challengeID + "-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)
	if err != nil {
		t.Fatalf(
			"create identifier unlink request %q: %v",
			challengeID,
			err,
		)
	}
}

func countIdentifierUnlinkCompletionIdentifier(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID string,
	identifier auth.Identifier,
) int {
	t.Helper()

	var count int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		`,
		identityID,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identity identifier: %v",
			err,
		)
	}

	return count
}

func assertIdentifierUnlinkCompletionChallengeUnverified(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	challengeID string,
) {
	t.Helper()

	var verifiedAt *time.Time

	err := pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&verifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query unlink challenge verification state: %v",
			err,
		)
	}

	if verifiedAt != nil {
		t.Fatal(
			"failed unlink completion consumed the OTP challenge",
		)
	}
}
