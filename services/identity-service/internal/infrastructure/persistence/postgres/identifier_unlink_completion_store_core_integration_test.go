//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentifierUnlinkCompletionStoreCompletesAtomically(
	t *testing.T,
) {
	ctx, pool, requestStore :=
		newIdentifierUnlinkRequestIntegrationTest(t)

	completionStore :=
		NewIdentifierUnlinkCompletionStore(pool)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	cleanupIdentifierUnlinkOutboxEvents(
		t,
		pool,
		identityID,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000501",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-success@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_success"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	verifiedAt := time.Now().UTC()

	err := completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() returned an error: %v",
			err,
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	) != 0 {
		t.Fatal(
			"target identifier still exists after unlink completion",
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	) != 1 {
		t.Fatal(
			"verification identifier was removed during unlink completion",
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
			"query completed unlink challenge: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"unlink completion did not mark OTP challenge verified",
		)
	}

	var operationCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	)
	if err != nil {
		t.Fatalf(
			"query completed unlink operation: %v",
			err,
		)
	}

	if operationCount != 1 {
		t.Fatalf(
			"unlink operation rows = %d, want 1",
			operationCount,
		)
	}

	assertIdentifierUnlinkedOutboxEvent(
		t,
		pool,
		identityID,
		targetIdentifier.Type,
	)

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  verifiedAt.Add(time.Second),
		},
	)

	if !errors.Is(
		err,
		auth.ErrChallengeUsed,
	) {
		t.Fatalf(
			"second Complete() error = %v, want %v",
			err,
			auth.ErrChallengeUsed,
		)
	}

	eventCount := countIdentifierUnlinkedOutboxEvents(
		t,
		pool,
		identityID,
	)

	if eventCount != 1 {
		t.Fatalf(
			"identifier unlinked outbox event count after retry = %d, want 1",
			eventCount,
		)
	}
}

func TestIdentifierUnlinkCompletionStoreRejectsMissingTargetIdentifier(
	t *testing.T,
) {
	ctx, pool, requestStore :=
		newIdentifierUnlinkRequestIntegrationTest(t)

	completionStore :=
		NewIdentifierUnlinkCompletionStore(pool)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	cleanupIdentifierUnlinkOutboxEvents(
		t,
		pool,
		identityID,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000502",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-target-missing@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_target_missing"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	_, err := pool.Exec(
		ctx,
		`
			DELETE FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		`,
		identityID,
		string(targetIdentifier.Type),
		targetIdentifier.Value,
	)
	if err != nil {
		t.Fatalf(
			"delete target identifier before completion: %v",
			err,
		)
	}

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierNotLinked,
	) {
		t.Fatalf(
			"Complete() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	assertIdentifierUnlinkCompletionChallengeUnverified(
		t,
		ctx,
		pool,
		challengeID,
	)

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	) != 1 {
		t.Fatal(
			"verification identifier changed after failed completion",
		)
	}

	eventCount := countIdentifierUnlinkedOutboxEvents(
		t,
		pool,
		identityID,
	)

	if eventCount != 0 {
		t.Fatalf(
			"identifier unlinked outbox event count = %d, want 0",
			eventCount,
		)
	}
}

func TestIdentifierUnlinkCompletionStoreRejectsMissingVerificationIdentifier(
	t *testing.T,
) {
	ctx, pool, requestStore :=
		newIdentifierUnlinkRequestIntegrationTest(t)

	completionStore :=
		NewIdentifierUnlinkCompletionStore(pool)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	cleanupIdentifierUnlinkOutboxEvents(
		t,
		pool,
		identityID,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000503",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-verification-missing@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_verification_missing"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	_, err := pool.Exec(
		ctx,
		`
			DELETE FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		`,
		identityID,
		string(verificationIdentifier.Type),
		verificationIdentifier.Value,
	)
	if err != nil {
		t.Fatalf(
			"delete verification identifier before completion: %v",
			err,
		)
	}

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierNotLinked,
	) {
		t.Fatalf(
			"Complete() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	) != 1 {
		t.Fatal(
			"target identifier was deleted after verification identifier disappeared",
		)
	}

	assertIdentifierUnlinkCompletionChallengeUnverified(
		t,
		ctx,
		pool,
		challengeID,
	)

	eventCount := countIdentifierUnlinkedOutboxEvents(
		t,
		pool,
		identityID,
	)

	if eventCount != 0 {
		t.Fatalf(
			"identifier unlinked outbox event count = %d, want 0",
			eventCount,
		)
	}
}

func assertIdentifierUnlinkedOutboxEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	identityID string,
	identifierType auth.IdentifierType,
) {
	t.Helper()

	var aggregateType string
	var aggregateID string
	var eventType string
	var schemaVersion int16
	var payloadIdentityID string
	var payloadIdentifierType string
	var published bool
	var publishAttempts int

	err := pool.QueryRow(
		context.Background(),
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
		string(auth.IdentityDomainEventIdentifierUnlinked),
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
			"query identifier unlinked outbox event: %v",
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
		auth.IdentityDomainEventIdentifierUnlinked,
	) {
		t.Fatalf(
			"event type = %q, want %q",
			eventType,
			auth.IdentityDomainEventIdentifierUnlinked,
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
			"identifier unlinked outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, want 0",
			publishAttempts,
		)
	}
}

func countIdentifierUnlinkedOutboxEvents(
	t *testing.T,
	pool *pgxpool.Pool,
	identityID string,
) int {
	t.Helper()

	var count int

	err := pool.QueryRow(
		context.Background(),
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		identityID,
		string(auth.IdentityDomainEventIdentifierUnlinked),
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identifier unlinked outbox events: %v",
			err,
		)
	}

	return count
}

func cleanupIdentifierUnlinkOutboxEvents(
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
			string(auth.IdentityDomainEventIdentifierUnlinked),
		)
		if err != nil {
			t.Errorf(
				"clean identifier unlinked outbox events: %v",
				err,
			)
		}
	})
}
