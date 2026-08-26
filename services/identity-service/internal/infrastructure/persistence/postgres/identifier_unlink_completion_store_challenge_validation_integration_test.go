//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentifierUnlinkCompletionStoreRejectsInvalidChallengeStates(
	t *testing.T,
) {
	tests := []struct {
		name      string
		challenge string
		mutate    func(
			context.Context,
			*pgxpool.Pool,
			string,
		) error
		expected error
	}{
		{
			name:      "used",
			challenge: "otp_ch_unlink_completion_used",
			mutate: func(
				ctx context.Context,
				pool *pgxpool.Pool,
				challengeID string,
			) error {
				_, err := pool.Exec(
					ctx,
					`
						UPDATE otp_challenges
						SET verified_at = $1
						WHERE id = $2
					`,
					time.Now().UTC(),
					challengeID,
				)

				return err
			},
			expected: auth.ErrChallengeUsed,
		},
		{
			name:      "cancelled",
			challenge: "otp_ch_unlink_completion_cancelled",
			mutate: func(
				ctx context.Context,
				pool *pgxpool.Pool,
				challengeID string,
			) error {
				_, err := pool.Exec(
					ctx,
					`
						UPDATE otp_challenges
						SET cancelled_at = $1
						WHERE id = $2
					`,
					time.Now().UTC(),
					challengeID,
				)

				return err
			},
			expected: auth.ErrChallengeCancelled,
		},
		{
			name:      "expired",
			challenge: "otp_ch_unlink_completion_expired",
			mutate: func(
				ctx context.Context,
				pool *pgxpool.Pool,
				challengeID string,
			) error {
				return nil
			},
			expected: auth.ErrChallengeExpired,
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, pool, requestStore :=
				newIdentifierUnlinkRequestIntegrationTest(t)

			completionStore :=
				NewIdentifierUnlinkCompletionStore(pool)

			identityID :=
				createIdentifierUnlinkTestIdentity(
					t,
					ctx,
					pool,
				)

			targetIdentifier := auth.Identifier{
				Type: auth.IdentifierTypePhone,
				Value: []string{
					"+9647500000504",
					"+9647500000505",
					"+9647500000506",
				}[index],
			}

			verificationIdentifier := auth.Identifier{
				Type: auth.IdentifierTypeEmail,
				Value: []string{
					"unlink-completion-used@example.com",
					"unlink-completion-cancelled@example.com",
					"unlink-completion-expired@example.com",
				}[index],
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

			createIdentifierUnlinkCompletionRequest(
				t,
				ctx,
				requestStore,
				identityID,
				tt.challenge,
				targetIdentifier,
				verificationIdentifier,
			)

			if err := tt.mutate(
				ctx,
				pool,
				tt.challenge,
			); err != nil {
				t.Fatalf(
					"mutate challenge state: %v",
					err,
				)
			}

			verifiedAt := time.Now().UTC()

			if tt.name == "expired" {
				verifiedAt = verifiedAt.Add(10 * time.Minute)
			}

			err := completionStore.Complete(
				ctx,
				auth.IdentifierUnlinkCompletionInput{
					ChallengeID: tt.challenge,
					IdentityID:  identityID,
					VerifiedAt:  verifiedAt,
				},
			)

			if !errors.Is(err, tt.expected) {
				t.Fatalf(
					"Complete() error = %v, want %v",
					err,
					tt.expected,
				)
			}

			if countIdentifierUnlinkCompletionIdentifier(
				t,
				ctx,
				pool,
				identityID,
				targetIdentifier,
			) != 1 {
				t.Fatal(
					"failed completion deleted target identifier",
				)
			}
		})
	}
}

func TestIdentifierUnlinkCompletionStoreRejectsWrongPurposeAndRollsBack(
	t *testing.T,
) {
	ctx, pool, requestStore :=
		newIdentifierUnlinkRequestIntegrationTest(t)

	completionStore :=
		NewIdentifierUnlinkCompletionStore(pool)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000507",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-purpose@example.com",
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

	const challengeID = "otp_ch_unlink_completion_wrong_purpose"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	_, err := pool.Exec(
		ctx,
		`
			UPDATE otp_challenges
			SET purpose = $1
			WHERE id = $2
		`,
		string(auth.OTPPurposeLinkIdentifier),
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"change challenge purpose: %v",
			err,
		)
	}

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)
	if err == nil {
		t.Fatal(
			"Complete() accepted a non-unlink challenge",
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	) != 1 {
		t.Fatal(
			"wrong-purpose completion deleted target identifier",
		)
	}

	assertIdentifierUnlinkCompletionChallengeUnverified(
		t,
		ctx,
		pool,
		challengeID,
	)
}
