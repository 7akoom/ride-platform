package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestNewIdentifierLinkCompletionStorePanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewIdentifierLinkCompletionStore() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewIdentifierLinkCompletionStore(nil)
}

func TestIdentifierLinkCompletionStoreRejectsInvalidInput(
	t *testing.T,
) {
	validInput := func() auth.IdentifierLinkCompletionInput {
		return auth.IdentifierLinkCompletionInput{
			ChallengeID: "otp_ch_link_test",
			IdentityID:  "11111111-1111-1111-1111-111111111111",
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "user@example.com",
			},
			VerifiedAt: time.Now().UTC(),
		}
	}

	tests := []struct {
		name  string
		input auth.IdentifierLinkCompletionInput
	}{
		{
			name: "blank challenge ID",
			input: func() auth.IdentifierLinkCompletionInput {
				input := validInput()
				input.ChallengeID = " \t\n "
				return input
			}(),
		},
		{
			name: "blank identity ID",
			input: func() auth.IdentifierLinkCompletionInput {
				input := validInput()
				input.IdentityID = "   "
				return input
			}(),
		},
		{
			name: "invalid identifier type",
			input: func() auth.IdentifierLinkCompletionInput {
				input := validInput()
				input.Identifier.Type =
					auth.IdentifierType("username")
				return input
			}(),
		},
		{
			name: "invalid phone identifier",
			input: func() auth.IdentifierLinkCompletionInput {
				input := validInput()
				input.Identifier = auth.Identifier{
					Type:  auth.IdentifierTypePhone,
					Value: "07501234567",
				}
				return input
			}(),
		},
		{
			name: "invalid email identifier",
			input: func() auth.IdentifierLinkCompletionInput {
				input := validInput()
				input.Identifier = auth.Identifier{
					Type:  auth.IdentifierTypeEmail,
					Value: "not-an-email",
				}
				return input
			}(),
		},
		{
			name: "zero verification time",
			input: func() auth.IdentifierLinkCompletionInput {
				input := validInput()
				input.VerifiedAt = time.Time{}
				return input
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &IdentifierLinkCompletionStore{}

			err := store.Complete(
				context.Background(),
				tt.input,
			)

			if err == nil {
				t.Fatal(
					"Complete() accepted invalid identifier link completion input",
				)
			}
		})
	}
}
