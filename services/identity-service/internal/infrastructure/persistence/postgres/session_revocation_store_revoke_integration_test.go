//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestSessionRevocationStoreRevokesSessionAndAllRefreshTokens(
	t *testing.T,
) {
	fixture := newSessionRevocationIntegrationFixture(
		t,
		"+9647500000051",
	)

	cleanupSessionRevokedOutboxEvents(
		t,
		fixture,
	)

	firstTokenHash := strings.Repeat(
		"c",
		64,
	)

	secondTokenHash := strings.Repeat(
		"d",
		64,
	)

	refreshExpiresAt := fixture.now.Add(
		29 * 24 * time.Hour,
	)

	fixture.createRefreshToken(
		firstTokenHash,
		refreshExpiresAt,
	)

	fixture.createRefreshToken(
		secondTokenHash,
		refreshExpiresAt,
	)

	store := NewSessionRevocationStore(
		fixture.pool,
	)

	target, found, err :=
		store.FindRevocationTargetByRefreshTokenHash(
			fixture.ctx,
			firstTokenHash,
		)
	if err != nil {
		t.Fatalf(
			"FindRevocationTargetByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindRevocationTargetByRefreshTokenHash() did not find known refresh token",
		)
	}

	if target.SessionID != fixture.sessionID {
		t.Fatalf(
			"revocation target session ID = %q, expected %q",
			target.SessionID,
			fixture.sessionID,
		)
	}

	if !target.SessionExpiresAt.Equal(
		fixture.sessionExpiresAt,
	) {
		t.Fatalf(
			"revocation target expiration = %v, expected %v",
			target.SessionExpiresAt,
			fixture.sessionExpiresAt,
		)
	}

	unknownTokenHash := strings.Repeat(
		"e",
		64,
	)

	unknownTarget, found, err :=
		store.FindRevocationTargetByRefreshTokenHash(
			fixture.ctx,
			unknownTokenHash,
		)
	if err != nil {
		t.Fatalf(
			"unknown refresh token lookup returned an error: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"unknown refresh token lookup unexpectedly found a session",
		)
	}

	if unknownTarget.SessionID != "" ||
		!unknownTarget.SessionExpiresAt.IsZero() {
		t.Fatalf(
			"unknown refresh token target = %+v, expected zero value",
			unknownTarget,
		)
	}

	revokedAt := fixture.now.Add(
		time.Minute,
	)

	err = store.RevokeSessionByRefreshTokenHash(
		fixture.ctx,
		firstTokenHash,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSessionByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	sessionRevokedAt := fixture.sessionRevokedAt()

	if sessionRevokedAt == nil {
		t.Fatal(
			"authentication session was not revoked",
		)
	}

	if !sessionRevokedAt.Equal(revokedAt) {
		t.Fatalf(
			"session revoked at = %v, expected %v",
			sessionRevokedAt,
			revokedAt,
		)
	}

	totalRefreshTokens, revokedRefreshTokens, activeRefreshTokens :=
		fixture.refreshTokenCounts()

	if totalRefreshTokens != 2 {
		t.Fatalf(
			"refresh token count = %d, expected 2",
			totalRefreshTokens,
		)
	}

	if revokedRefreshTokens != 2 {
		t.Fatalf(
			"revoked refresh token count = %d, expected 2",
			revokedRefreshTokens,
		)
	}

	if activeRefreshTokens != 0 {
		t.Fatalf(
			"active refresh token count = %d, expected 0",
			activeRefreshTokens,
		)
	}

	assertSessionRevokedOutboxEvent(
		t,
		fixture,
		revokedAt,
	)

	eventCount := countSessionRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"identity.session_revoked outbox event count = %d, expected 1",
			eventCount,
		)
	}

	secondRevokedAt := revokedAt.Add(
		time.Second,
	)

	err = store.RevokeSessionByRefreshTokenHash(
		fixture.ctx,
		firstTokenHash,
		secondRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"second logout returned an error: %v",
			err,
		)
	}

	sessionRevokedAt = fixture.sessionRevokedAt()

	if sessionRevokedAt == nil {
		t.Fatal(
			"authentication session lost revocation timestamp after second logout",
		)
	}

	if !sessionRevokedAt.Equal(revokedAt) {
		t.Fatalf(
			"session revoked at after second logout = %v, expected original %v",
			sessionRevokedAt,
			revokedAt,
		)
	}

	eventCount = countSessionRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"identity.session_revoked outbox event count after second logout = %d, expected 1",
			eventCount,
		)
	}

	err = store.RevokeSessionByRefreshTokenHash(
		fixture.ctx,
		unknownTokenHash,
		revokedAt.Add(2*time.Second),
	)
	if err != nil {
		t.Fatalf(
			"unknown refresh token returned an error: %v",
			err,
		)
	}

	eventCount = countSessionRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"identity.session_revoked outbox event count after unknown token = %d, expected 1",
			eventCount,
		)
	}
}

func assertSessionRevokedOutboxEvent(
	t *testing.T,
	fixture *sessionRevocationIntegrationFixture,
	occurredAt time.Time,
) {
	t.Helper()

	var (
		aggregateType     string
		aggregateID       string
		eventType         string
		schemaVersion     int16
		payloadIdentityID string
		payloadSessionID  string
		storedOccurredAt  time.Time
		published         bool
		publishAttempts   int
	)

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				aggregate_type,
				aggregate_id::text,
				event_type,
				schema_version,
				payload ->> 'identity_id',
				payload ->> 'session_id',
				occurred_at,
				published_at IS NOT NULL,
				publish_attempts
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		fixture.identityID,
		string(
			auth.IdentityDomainEventSessionRevoked,
		),
	).Scan(
		&aggregateType,
		&aggregateID,
		&eventType,
		&schemaVersion,
		&payloadIdentityID,
		&payloadSessionID,
		&storedOccurredAt,
		&published,
		&publishAttempts,
	)
	if err != nil {
		t.Fatalf(
			"query identity.session_revoked outbox event: %v",
			err,
		)
	}

	if aggregateType != identityOutboxAggregateType {
		t.Fatalf(
			"aggregate type = %q, expected %q",
			aggregateType,
			identityOutboxAggregateType,
		)
	}

	if aggregateID != fixture.identityID {
		t.Fatalf(
			"aggregate ID = %q, expected %q",
			aggregateID,
			fixture.identityID,
		)
	}

	if eventType != string(
		auth.IdentityDomainEventSessionRevoked,
	) {
		t.Fatalf(
			"event type = %q, expected %q",
			eventType,
			auth.IdentityDomainEventSessionRevoked,
		)
	}

	if schemaVersion !=
		auth.IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, expected %d",
			schemaVersion,
			auth.IdentityDomainEventSchemaVersion,
		)
	}

	if payloadIdentityID != fixture.identityID {
		t.Fatalf(
			"payload identity ID = %q, expected %q",
			payloadIdentityID,
			fixture.identityID,
		)
	}

	if payloadSessionID != fixture.sessionID {
		t.Fatalf(
			"payload session ID = %q, expected %q",
			payloadSessionID,
			fixture.sessionID,
		)
	}

	if !storedOccurredAt.Equal(occurredAt) {
		t.Fatalf(
			"event occurred at = %v, expected %v",
			storedOccurredAt,
			occurredAt,
		)
	}

	if published {
		t.Fatal(
			"identity.session_revoked outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, expected 0",
			publishAttempts,
		)
	}
}

func countSessionRevokedOutboxEvents(
	t *testing.T,
	fixture *sessionRevocationIntegrationFixture,
) int {
	t.Helper()

	var count int

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		fixture.identityID,
		string(
			auth.IdentityDomainEventSessionRevoked,
		),
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identity.session_revoked outbox events: %v",
			err,
		)
	}

	return count
}

func cleanupSessionRevokedOutboxEvents(
	t *testing.T,
	fixture *sessionRevocationIntegrationFixture,
) {
	t.Helper()

	_, err := fixture.pool.Exec(
		fixture.ctx,
		`
			DELETE FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		fixture.identityID,
		string(
			auth.IdentityDomainEventSessionRevoked,
		),
	)
	if err != nil {
		t.Fatalf(
			"clean existing identity.session_revoked outbox events: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := fixture.pool.Exec(
			fixture.ctx,
			`
				DELETE FROM outbox_events
				WHERE aggregate_type = $1
				  AND aggregate_id = $2::uuid
				  AND event_type = $3
			`,
			identityOutboxAggregateType,
			fixture.identityID,
			string(
				auth.IdentityDomainEventSessionRevoked,
			),
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean identity.session_revoked outbox events: %v",
				cleanupErr,
			)
		}
	})
}
