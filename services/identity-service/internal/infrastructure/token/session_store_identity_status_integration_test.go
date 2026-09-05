//go:build integration

package token

import (
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestSessionStoreRejectsInactiveIdentity(
	t *testing.T,
) {
	testCases := []struct {
		name        string
		phoneNumber string
		status      auth.IdentityStatus
	}{
		{
			name:        "suspended identity",
			phoneNumber: "+9647500000061",
			status:      auth.IdentityStatusSuspended,
		},
		{
			name:        "disabled identity",
			phoneNumber: "+9647500000062",
			status:      auth.IdentityStatusDisabled,
		},
	}

	for _, testCase := range testCases {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				fixture := newSessionStoreIntegrationFixture(
					t,
					testCase.phoneNumber,
				)

				_, err := fixture.pool.Exec(
					fixture.ctx,
					`
						UPDATE identities
						SET
							status = $2,
							updated_at = CURRENT_TIMESTAMP
						WHERE id = $1::uuid
					`,
					fixture.identityID,
					string(testCase.status),
				)
				if err != nil {
					t.Fatalf(
						"set identity status: %v",
						err,
					)
				}

				var challengeBaseTime time.Time

				err = fixture.pool.QueryRow(
					fixture.ctx,
					"SELECT CURRENT_TIMESTAMP",
				).Scan(
					&challengeBaseTime,
				)
				if err != nil {
					t.Fatalf(
						"query challenge base time: %v",
						err,
					)
				}

				challengeBaseTime = challengeBaseTime.UTC()

				challengeID := "otp_ch_inactive_identity_" +
					string(testCase.status)

				fixture.createOTPChallenge(
					challengeID,
					"test-code-hash",
					challengeBaseTime.Add(5*time.Minute),
				)

				var now time.Time

				err = fixture.pool.QueryRow(
					fixture.ctx,
					"SELECT CURRENT_TIMESTAMP",
				).Scan(
					&now,
				)
				if err != nil {
					t.Fatalf(
						"query verification time: %v",
						err,
					)
				}

				now = now.UTC()

				store := NewSessionStore(
					fixture.pool,
				)

				_, err = store.Create(
					fixture.ctx,
					SessionCreationInput{
						ChallengeID:      challengeID,
						VerifiedAt:       now,
						SessionID:        fixture.generateSessionID(),
						IdentityID:       fixture.identityID,
						SessionExpiresAt: now.Add(30 * 24 * time.Hour),
						RefreshTokenHash: "test-refresh-token-hash-" +
							string(testCase.status),
						RefreshTokenExpiresAt: now.Add(
							29 * 24 * time.Hour,
						),
					},
				)

				if !errors.Is(
					err,
					auth.ErrIdentityInactive,
				) {
					t.Fatalf(
						"Create() error = %v, expected %v",
						err,
						auth.ErrIdentityInactive,
					)
				}

				if fixture.countAuthSessions() != 0 {
					t.Fatal(
						"inactive identity unexpectedly created an authentication session",
					)
				}

				if fixture.countRefreshTokens() != 0 {
					t.Fatal(
						"inactive identity unexpectedly created a refresh token",
					)
				}

				if fixture.challengeVerifiedAt(
					challengeID,
				) != nil {
					t.Fatal(
						"OTP challenge remained verified after transaction rollback",
					)
				}
			},
		)
	}
}
