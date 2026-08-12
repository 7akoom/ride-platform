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
			ID:          "otp_ch_test",
			PhoneNumber: "+9647500000000",
			CodeHash:    "valid-code-hash",
			ExpiresAt:   time.Now().UTC().Add(5 * time.Minute),
		}
	}

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
			name: "blank phone number",
			challenge: func() auth.OTPChallenge {
				challenge := validChallenge()
				challenge.PhoneNumber = "\t\n "
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
