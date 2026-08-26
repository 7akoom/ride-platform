package grpc

import (
	"context"
	"testing"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentityHandlerGetMyIdentityReturnsAuthenticatedIdentity(
	t *testing.T,
) {
	verifiedAt := time.Date(
		2026,
		time.August,
		16,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	authService := &fakeAuthService{
		getMyIdentityResult: auth.IdentityDetails{
			ID:     "11111111-1111-1111-1111-111111111111",
			Status: auth.IdentityStatusActive,
			Identifiers: []auth.IdentityDetailsIdentifier{
				{
					Identifier: auth.Identifier{
						Type:  auth.IdentifierTypePhone,
						Value: "+9647501234567",
					},
					VerifiedAt: verifiedAt,
				},
				{
					Identifier: auth.Identifier{
						Type:  auth.IdentifierTypeEmail,
						Value: "user@example.com",
					},
					VerifiedAt: verifiedAt.Add(time.Minute),
				},
			},
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.GetMyIdentity(
		ctx,
		&identityv1.GetMyIdentityRequest{},
	)
	if err != nil {
		t.Fatalf(
			"GetMyIdentity() returned an error: %v",
			err,
		)
	}

	if !authService.getMyIdentityCalled {
		t.Fatal("GetMyIdentity() did not call auth service")
	}

	if authService.getMyIdentityInput.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"auth service identity ID = %q",
			authService.getMyIdentityInput.IdentityID,
		)
	}

	if response.GetIdentityId() !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"response identity ID = %q",
			response.GetIdentityId(),
		)
	}

	if response.GetStatus() !=
		identityv1.IdentityStatus_IDENTITY_STATUS_ACTIVE {
		t.Fatalf(
			"response status = %v",
			response.GetStatus(),
		)
	}

	if len(response.GetIdentifiers()) != 2 {
		t.Fatalf(
			"identifiers count = %d, want 2",
			len(response.GetIdentifiers()),
		)
	}

	if response.GetIdentifiers()[0].GetType() !=
		identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE {
		t.Fatalf(
			"first identifier type = %v",
			response.GetIdentifiers()[0].GetType(),
		)
	}

	if response.GetIdentifiers()[0].GetValue() !=
		"+9647501234567" {
		t.Fatalf(
			"first identifier value = %q",
			response.GetIdentifiers()[0].GetValue(),
		)
	}

	if !response.GetIdentifiers()[0].
		GetVerifiedAt().
		AsTime().
		Equal(verifiedAt) {
		t.Fatalf(
			"first identifier verified_at = %v, want %v",
			response.GetIdentifiers()[0].GetVerifiedAt().AsTime(),
			verifiedAt,
		)
	}

	if response.GetIdentifiers()[1].GetType() !=
		identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL {
		t.Fatalf(
			"second identifier type = %v",
			response.GetIdentifiers()[1].GetType(),
		)
	}

	if response.GetIdentifiers()[1].GetValue() !=
		"user@example.com" {
		t.Fatalf(
			"second identifier value = %q",
			response.GetIdentifiers()[1].GetValue(),
		)
	}
}
