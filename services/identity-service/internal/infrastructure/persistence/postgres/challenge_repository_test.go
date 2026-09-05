package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestChallengeRepositoryRecordFailedAttemptRejectsZeroTime(
	t *testing.T,
) {
	repository := &ChallengeRepository{}

	err := repository.RecordFailedAttempt(
		context.Background(),
		"otp_ch_test",
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"RecordFailedAttempt() accepted a zero attempt time",
		)
	}
}

func TestChallengeRepositoryMarkVerifiedRejectsZeroTime(
	t *testing.T,
) {
	repository := &ChallengeRepository{}

	err := repository.MarkVerified(
		context.Background(),
		"otp_ch_test",
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"MarkVerified() accepted a zero verification time",
		)
	}
}

func TestChallengeRepositoryCancelRejectsZeroTime(
	t *testing.T,
) {
	repository := &ChallengeRepository{}

	err := repository.Cancel(
		context.Background(),
		"otp_ch_test",
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"Cancel() accepted a zero cancellation time",
		)
	}
}

func TestNewChallengeRepositoryPanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewChallengeRepository() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewChallengeRepository(nil)
}

func TestChallengeRepositoryCreateRejectsInvalidInput(
	t *testing.T,
) {
	validChallenge := func() auth.OTPChallenge {
		return auth.OTPChallenge{
			ID: "otp_ch_test",
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647500000000",
			},
			Purpose:  auth.OTPPurposeLogin,
			CodeHash: "valid-code-hash",
			ExpiresAt: time.Now().
				UTC().
				Add(5 * time.Minute),
		}
	}

	targetIdentityID := "11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name      string
		challenge auth.OTPChallenge
	}{
		{
			name: "blank ID",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.ID = "   "
				return challenge
			}(),
		},
		{
			name: "invalid identifier type",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.Identifier.Type =
					auth.IdentifierType("username")
				return challenge
			}(),
		},
		{
			name: "invalid phone identifier",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.Identifier.Value =
					"07500000000"
				return challenge
			}(),
		},
		{
			name: "invalid purpose",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.Purpose =
					auth.OTPPurpose("unknown")
				return challenge
			}(),
		},
		{
			name: "login purpose cannot target identity",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.TargetIdentityID =
					&targetIdentityID
				return challenge
			}(),
		},
		{
			name: "link identifier requires target identity",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.Purpose =
					auth.OTPPurposeLinkIdentifier
				return challenge
			}(),
		},
		{
			name: "link identifier rejects blank target identity",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.Purpose =
					auth.OTPPurposeLinkIdentifier

				blankTarget := "   "
				challenge.TargetIdentityID =
					&blankTarget

				return challenge
			}(),
		},
		{
			name: "blank code hash",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.CodeHash = ""
				return challenge
			}(),
		},
		{
			name: "zero expiration",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.ExpiresAt = time.Time{}
				return challenge
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &ChallengeRepository{}

			err := repository.Create(
				context.Background(),
				tt.challenge,
			)

			if err == nil {
				t.Fatal(
					"Create() accepted invalid OTP challenge input",
				)
			}
		})
	}
}
