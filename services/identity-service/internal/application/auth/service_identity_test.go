package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGetMyIdentityReturnsIdentityDetails(
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

	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID:     "11111111-1111-1111-1111-111111111111",
			Status: IdentityStatusActive,
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: Identifier{
						Type:  IdentifierTypePhone,
						Value: "+9647501234567",
					},
					VerifiedAt: verifiedAt,
				},
				{
					Identifier: Identifier{
						Type:  IdentifierTypeEmail,
						Value: "user@example.com",
					},
					VerifiedAt: verifiedAt.Add(time.Minute),
				},
			},
		},
		findFound: true,
	}

	dependencies := newValidServiceConstructorTestDependencies()
	dependencies.identityReader = identityReader

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	result, err := service.GetMyIdentity(
		context.Background(),
		GetMyIdentityInput{
			IdentityID: "  11111111-1111-1111-1111-111111111111  ",
		},
	)
	if err != nil {
		t.Fatalf(
			"GetMyIdentity() returned an error: %v",
			err,
		)
	}

	if identityReader.findCalls != 1 {
		t.Fatalf(
			"identity reader calls = %d, want 1",
			identityReader.findCalls,
		)
	}

	if identityReader.findIdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity reader identity ID = %q",
			identityReader.findIdentityID,
		)
	}

	if result.ID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity ID = %q",
			result.ID,
		)
	}

	if result.Status != IdentityStatusActive {
		t.Fatalf(
			"identity status = %q, want %q",
			result.Status,
			IdentityStatusActive,
		)
	}

	if len(result.Identifiers) != 2 {
		t.Fatalf(
			"identifiers count = %d, want 2",
			len(result.Identifiers),
		)
	}

	if result.Identifiers[0].Identifier.Type !=
		IdentifierTypePhone {
		t.Fatalf(
			"first identifier type = %q, want %q",
			result.Identifiers[0].Identifier.Type,
			IdentifierTypePhone,
		)
	}

	if result.Identifiers[1].Identifier.Type !=
		IdentifierTypeEmail {
		t.Fatalf(
			"second identifier type = %q, want %q",
			result.Identifiers[1].Identifier.Type,
			IdentifierTypeEmail,
		)
	}
}

func TestGetMyIdentityRejectsBlankIdentityID(
	t *testing.T,
) {
	identityReader := &testIdentityReader{}

	dependencies := newValidServiceConstructorTestDependencies()
	dependencies.identityReader = identityReader

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.GetMyIdentity(
		context.Background(),
		GetMyIdentityInput{
			IdentityID: "   ",
		},
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"GetMyIdentity() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}

	if identityReader.findCalls != 0 {
		t.Fatalf(
			"identity reader calls = %d, want 0",
			identityReader.findCalls,
		)
	}
}

func TestGetMyIdentityReturnsNotFoundForUnknownIdentity(
	t *testing.T,
) {
	identityReader := &testIdentityReader{
		findFound: false,
	}

	dependencies := newValidServiceConstructorTestDependencies()
	dependencies.identityReader = identityReader

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.GetMyIdentity(
		context.Background(),
		GetMyIdentityInput{
			IdentityID: "22222222-2222-2222-2222-222222222222",
		},
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"GetMyIdentity() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}

	if identityReader.findCalls != 1 {
		t.Fatalf(
			"identity reader calls = %d, want 1",
			identityReader.findCalls,
		)
	}
}

func TestGetMyIdentityPropagatesIdentityReaderError(
	t *testing.T,
) {
	readerError := errors.New(
		"identity reader unavailable",
	)

	identityReader := &testIdentityReader{
		findErr: readerError,
	}

	dependencies := newValidServiceConstructorTestDependencies()
	dependencies.identityReader = identityReader

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.GetMyIdentity(
		context.Background(),
		GetMyIdentityInput{
			IdentityID: "33333333-3333-3333-3333-333333333333",
		},
	)

	if !errors.Is(err, readerError) {
		t.Fatalf(
			"GetMyIdentity() error = %v, want wrapped %v",
			err,
			readerError,
		)
	}

	if identityReader.findCalls != 1 {
		t.Fatalf(
			"identity reader calls = %d, want 1",
			identityReader.findCalls,
		)
	}
}
