//go:build integration

package postgres

import (
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentityIdentifierRepositoryLinksEmailToExistingPhoneIdentity(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const phoneNumber = "+9647500000102"
	const emailAddress = "linked-email@example.com"

	fixture.prepareCleanup(
		phoneNumber,
		emailAddress,
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
			"create phone identity: %v",
			err,
		)
	}

	err = fixture.repository.LinkIdentifier(
		fixture.ctx,
		identity.ID,
		auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: emailAddress,
		},
		fixture.verifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"LinkIdentifier() returned an error: %v",
			err,
		)
	}

	err = fixture.repository.LinkIdentifier(
		fixture.ctx,
		identity.ID,
		auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: emailAddress,
		},
		fixture.verifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"idempotent LinkIdentifier() returned an error: %v",
			err,
		)
	}

	foundIdentity, found, err :=
		fixture.repository.FindIdentityByIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
		)
	if err != nil {
		t.Fatalf(
			"find identity by linked email: %v",
			err,
		)
	}

	if !found {
		t.Fatal("identity was not found by linked email")
	}

	if foundIdentity.ID != identity.ID {
		t.Fatalf(
			"linked email belongs to identity %q, want %q",
			foundIdentity.ID,
			identity.ID,
		)
	}
}

func TestIdentityIdentifierRepositoryLinksPhoneToEmailOnlyIdentity(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const emailAddress = "email-to-phone@example.com"
	const phoneNumber = "+9647500000103"

	fixture.prepareCleanup(
		emailAddress,
		phoneNumber,
	)

	identity, err :=
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
			"create email identity: %v",
			err,
		)
	}

	err = fixture.repository.LinkIdentifier(
		fixture.ctx,
		identity.ID,
		auth.Identifier{
			Type:  auth.IdentifierTypePhone,
			Value: phoneNumber,
		},
		fixture.verifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"link phone identifier: %v",
			err,
		)
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
			"find identity by linked phone: %v",
			err,
		)
	}

	if !found {
		t.Fatal("identity was not found by linked phone")
	}

	if foundIdentity.ID != identity.ID {
		t.Fatalf(
			"linked phone belongs to identity %q, want %q",
			foundIdentity.ID,
			identity.ID,
		)
	}
}

func TestIdentityIdentifierRepositoryRejectsIdentifierOwnedByAnotherIdentity(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const firstPhone = "+9647500000104"
	const secondPhone = "+9647500000105"
	const emailAddress = "ownership@example.com"

	fixture.prepareCleanup(
		firstPhone,
		secondPhone,
		emailAddress,
	)

	firstIdentity, err :=
		fixture.repository.CreateIdentityWithIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: firstPhone,
			},
			fixture.verifiedAt,
		)
	if err != nil {
		t.Fatalf(
			"create first identity: %v",
			err,
		)
	}

	secondIdentity, err :=
		fixture.repository.CreateIdentityWithIdentifier(
			fixture.ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: secondPhone,
			},
			fixture.verifiedAt,
		)
	if err != nil {
		t.Fatalf(
			"create second identity: %v",
			err,
		)
	}

	if err := fixture.repository.LinkIdentifier(
		fixture.ctx,
		firstIdentity.ID,
		auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: emailAddress,
		},
		fixture.verifiedAt,
	); err != nil {
		t.Fatalf(
			"link email to first identity: %v",
			err,
		)
	}

	err = fixture.repository.LinkIdentifier(
		fixture.ctx,
		secondIdentity.ID,
		auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: emailAddress,
		},
		fixture.verifiedAt,
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierAlreadyLinked,
	) {
		t.Fatalf(
			"LinkIdentifier() error = %v, want %v",
			err,
			auth.ErrIdentifierAlreadyLinked,
		)
	}
}
