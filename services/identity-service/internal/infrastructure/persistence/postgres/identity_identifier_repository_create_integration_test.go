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

	cleanupIdentityCreatedOutboxEvents(
		t,
		fixture,
		identity.ID,
	)

	if identity.ID == "" {
		t.Fatal("created identity has an empty ID")
	}

	if !identity.IsActive {
		t.Fatal("new identity is not active")
	}

	assertIdentityCreatedOutboxEvent(
		t,
		fixture,
		identity.ID,
	)

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

	cleanupIdentityCreatedOutboxEvents(
		t,
		fixture,
		identity.ID,
	)

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

	cleanupIdentityCreatedOutboxEvents(
		t,
		fixture,
		firstIdentity.ID,
	)

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

	var eventCount int

	if err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		firstIdentity.ID,
		string(auth.IdentityDomainEventCreated),
	).Scan(
		&eventCount,
	); err != nil {
		t.Fatalf(
			"count identity created outbox events: %v",
			err,
		)
	}

	if eventCount != 1 {
		t.Fatalf(
			"identity created outbox event count = %d, want 1",
			eventCount,
		)
	}
}

func assertIdentityCreatedOutboxEvent(
	t *testing.T,
	fixture *identityIdentifierRepositoryIntegrationFixture,
	identityID string,
) {
	t.Helper()

	var aggregateType string
	var aggregateID string
	var eventType string
	var schemaVersion int16
	var payloadIdentityID string
	var payloadStatus string
	var published bool
	var publishAttempts int

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				aggregate_type,
				aggregate_id::text,
				event_type,
				schema_version,
				payload ->> 'identity_id',
				payload ->> 'status',
				published_at IS NOT NULL,
				publish_attempts
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		identityID,
		string(auth.IdentityDomainEventCreated),
	).Scan(
		&aggregateType,
		&aggregateID,
		&eventType,
		&schemaVersion,
		&payloadIdentityID,
		&payloadStatus,
		&published,
		&publishAttempts,
	)
	if err != nil {
		t.Fatalf(
			"query identity created outbox event: %v",
			err,
		)
	}

	if aggregateType != identityOutboxAggregateType {
		t.Fatalf(
			"aggregate type = %q, want %q",
			aggregateType,
			identityOutboxAggregateType,
		)
	}

	if aggregateID != identityID {
		t.Fatalf(
			"aggregate ID = %q, want %q",
			aggregateID,
			identityID,
		)
	}

	if eventType != string(auth.IdentityDomainEventCreated) {
		t.Fatalf(
			"event type = %q, want %q",
			eventType,
			auth.IdentityDomainEventCreated,
		)
	}

	if schemaVersion != auth.IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			schemaVersion,
			auth.IdentityDomainEventSchemaVersion,
		)
	}

	if payloadIdentityID != identityID {
		t.Fatalf(
			"payload identity ID = %q, want %q",
			payloadIdentityID,
			identityID,
		)
	}

	if payloadStatus != string(auth.IdentityStatusActive) {
		t.Fatalf(
			"payload status = %q, want %q",
			payloadStatus,
			auth.IdentityStatusActive,
		)
	}

	if published {
		t.Fatal(
			"identity created outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, want 0",
			publishAttempts,
		)
	}
}

func cleanupIdentityCreatedOutboxEvents(
	t *testing.T,
	fixture *identityIdentifierRepositoryIntegrationFixture,
	identityID string,
) {
	t.Helper()

	t.Cleanup(func() {
		_, err := fixture.pool.Exec(
			fixture.ctx,
			`
				DELETE FROM outbox_events
				WHERE aggregate_type = $1
				  AND aggregate_id = $2::uuid
				  AND event_type = $3
			`,
			identityOutboxAggregateType,
			identityID,
			string(auth.IdentityDomainEventCreated),
		)
		if err != nil {
			t.Errorf(
				"clean identity created outbox events: %v",
				err,
			)
		}
	})
}
