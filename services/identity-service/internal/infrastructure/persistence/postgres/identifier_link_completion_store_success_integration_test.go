//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentifierLinkCompletionStoreLinksIdentifierAndConsumesChallenge(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	identityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	cleanupIdentifierLinkOutboxEvents(
		t,
		pool,
		identityID,
	)

	const challengeID = "otp_ch_link_completion_success"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "Link.Success@EXAMPLE.COM",
	}

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-completion-success-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create link identifier challenge: %v",
			err,
		)
	}

	verifiedAt := time.Now().UTC()

	err := store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() returned an error: %v",
			err,
		)
	}

	var storedIdentityID string
	var normalizedValue string
	var identifierVerifiedAt time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identity_id::text,
				normalized_value,
				verified_at
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(auth.IdentifierTypeEmail),
		"link.success@example.com",
	).Scan(
		&storedIdentityID,
		&normalizedValue,
		&identifierVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query linked identifier: %v",
			err,
		)
	}

	if storedIdentityID != identityID {
		t.Fatalf(
			"linked identity ID = %q, want %q",
			storedIdentityID,
			identityID,
		)
	}

	if normalizedValue != "link.success@example.com" {
		t.Fatalf(
			"normalized identifier = %q, want %q",
			normalizedValue,
			"link.success@example.com",
		)
	}

	var challengeVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query completed challenge: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"identifier was linked but challenge was not consumed",
		)
	}

	if identifierVerifiedAt.IsZero() {
		t.Fatal(
			"linked identifier has zero verification time",
		)
	}

	assertIdentifierLinkedOutboxEvent(
		t,
		pool,
		identityID,
		auth.IdentifierTypeEmail,
	)
}

func TestIdentifierLinkCompletionStoreAllowsExistingLinkForSameIdentity(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	identityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	cleanupIdentifierLinkOutboxEvents(
		t,
		pool,
		identityID,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000088",
	}

	_, err := pool.Exec(
		ctx,
		`
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1, $2, $3, $4)
		`,
		identityID,
		string(identifier.Type),
		identifier.Value,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf(
			"seed identifier already linked to same identity: %v",
			err,
		)
	}

	const challengeID = "otp_ch_link_completion_idempotent"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-completion-idempotent-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create idempotent link challenge: %v",
			err,
		)
	}

	err = store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() for existing same-identity link returned: %v",
			err,
		)
	}

	var identifierCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&identifierCount,
	)
	if err != nil {
		t.Fatalf(
			"count identifier rows: %v",
			err,
		)
	}

	if identifierCount != 1 {
		t.Fatalf(
			"identifier row count = %d, want 1",
			identifierCount,
		)
	}

	var challengeVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query idempotent challenge state: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"idempotent link did not consume its OTP challenge",
		)
	}

	var eventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		identityID,
		string(auth.IdentityDomainEventIdentifierLinked),
	).Scan(
		&eventCount,
	)
	if err != nil {
		t.Fatalf(
			"count identifier linked outbox events: %v",
			err,
		)
	}

	if eventCount != 0 {
		t.Fatalf(
			"identifier linked outbox event count = %d, want 0",
			eventCount,
		)
	}
}

func assertIdentifierLinkedOutboxEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	identityID string,
	identifierType auth.IdentifierType,
) {
	t.Helper()

	ctx := context.Background()

	var aggregateType string
	var aggregateID string
	var eventType string
	var schemaVersion int16
	var payloadIdentityID string
	var payloadIdentifierType string
	var published bool
	var publishAttempts int

	err := pool.QueryRow(
		ctx,
		`
			SELECT
				aggregate_type,
				aggregate_id::text,
				event_type,
				schema_version,
				payload ->> 'identity_id',
				payload ->> 'identifier_type',
				published_at IS NOT NULL,
				publish_attempts
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		identityID,
		string(auth.IdentityDomainEventIdentifierLinked),
	).Scan(
		&aggregateType,
		&aggregateID,
		&eventType,
		&schemaVersion,
		&payloadIdentityID,
		&payloadIdentifierType,
		&published,
		&publishAttempts,
	)
	if err != nil {
		t.Fatalf(
			"query identifier linked outbox event: %v",
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

	if eventType != string(
		auth.IdentityDomainEventIdentifierLinked,
	) {
		t.Fatalf(
			"event type = %q, want %q",
			eventType,
			auth.IdentityDomainEventIdentifierLinked,
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

	if payloadIdentifierType != string(identifierType) {
		t.Fatalf(
			"payload identifier type = %q, want %q",
			payloadIdentifierType,
			identifierType,
		)
	}

	if published {
		t.Fatal(
			"identifier linked outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, want 0",
			publishAttempts,
		)
	}
}

func cleanupIdentifierLinkOutboxEvents(
	t *testing.T,
	pool *pgxpool.Pool,
	identityID string,
) {
	t.Helper()

	t.Cleanup(func() {
		_, err := pool.Exec(
			context.Background(),
			`
				DELETE FROM outbox_events
				WHERE aggregate_type = $1
				  AND aggregate_id = $2::uuid
				  AND event_type = $3
			`,
			identityOutboxAggregateType,
			identityID,
			string(auth.IdentityDomainEventIdentifierLinked),
		)
		if err != nil {
			t.Errorf(
				"clean identifier linked outbox events: %v",
				err,
			)
		}
	})
}
