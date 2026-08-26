//go:build integration

package postgres

import (
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentityIdentifierRepositoryCreatesAndFindsIdentityByPhone(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const phoneNumber = "+9647500000101"

	fixture.prepareCleanup(
		phoneNumber,
	)

	identity, err :=
		fixture.repository.CreateIdentityWithIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
			fixture.verifiedAt,
		)
	if err != nil {
		t.Fatalf(
			"CreateIdentityWithIdentifier() returned an error: %v",
			err,
		)
	}

	if identity.ID == "" {
		t.Fatal("created identity has an empty ID")
	}

	if !identity.IsActive {
		t.Fatal("new identity is not active")
	}

	foundIdentity, found, err :=
		fixture.repository.FindIdentityByIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
		)
	if err != nil {
		t.Fatalf(
			"FindIdentityByIdentifier() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal("created phone identity was not found")
	}

	if foundIdentity.ID != identity.ID {
		t.Fatalf(
			"found identity ID = %q, want %q",
			foundIdentity.ID,
			identity.ID,
		)
	}
}

func TestIdentityIdentifierRepositoryCreatesEmailOnlyIdentity(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const inputEmail = "EmailOnly@EXAMPLE.COM"
	const canonicalEmail = "emailonly@example.com"

	fixture.prepareCleanup(
		canonicalEmail,
	)

	identity, err :=
		fixture.repository.CreateIdentityWithIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: inputEmail,
			},
			fixture.verifiedAt,
		)
	if err != nil {
		t.Fatalf(
			"CreateIdentityWithIdentifier() returned an error: %v",
			err,
		)
	}

	if identity.ID == "" {
		t.Fatal("created identity has an empty ID")
	}

	if !identity.IsActive {
		t.Fatal("new email identity is not active")
	}

	foundIdentity, found, err :=
		fixture.repository.FindIdentityByIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "EMAILONLY@example.com",
			},
		)
	if err != nil {
		t.Fatalf(
			"FindIdentityByIdentifier() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal("created email identity was not found")
	}

	if foundIdentity.ID != identity.ID {
		t.Fatalf(
			"found identity ID = %q, want %q",
			foundIdentity.ID,
			identity.ID,
		)
	}
}

func TestIdentityIdentifierRepositoryCreateIsIdempotentForSameIdentifier(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const emailAddress = "idempotent@example.com"

	fixture.prepareCleanup(
		emailAddress,
	)

	firstIdentity, err :=
		fixture.repository.CreateIdentityWithIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			fixture.verifiedAt,
		)
	if err != nil {
		t.Fatalf(
			"first CreateIdentityWithIdentifier() returned an error: %v",
			err,
		)
	}

	secondIdentity, err :=
		fixture.repository.CreateIdentityWithIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			fixture.verifiedAt,
		)
	if err != nil {
		t.Fatalf(
			"second CreateIdentityWithIdentifier() returned an error: %v",
			err,
		)
	}

	if firstIdentity.ID != secondIdentity.ID {
		t.Fatalf(
			"same identifier returned different identities: first=%q second=%q",
			firstIdentity.ID,
			secondIdentity.ID,
		)
	}

	var count int

	if err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identifier_type = 'email'
			  AND normalized_value = $1
		`,
		emailAddress,
	).Scan(
		&count,
	); err != nil {
		t.Fatalf(
			"count idempotent identifier records: %v",
			err,
		)
	}

	if count != 1 {
		t.Fatalf(
			"identifier record count = %d, want 1",
			count,
		)
	}
}
