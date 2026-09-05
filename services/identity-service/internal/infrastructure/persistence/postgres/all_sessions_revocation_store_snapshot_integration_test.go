//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestAllSessionsRevocationStoreRevokesOnlySnapshotSessions(
	t *testing.T,
) {
	fixture := newAllSessionsRevocationTestFixture(
		t,
		"+9647500000153",
	)

	cleanupIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	firstTokenHash := strings.Repeat(
		"a",
		64,
	)

	secondTokenHash := strings.Repeat(
		"b",
		64,
	)

	thirdTokenHash := strings.Repeat(
		"c",
		64,
	)

	firstSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now,
			firstTokenHash,
		)

	secondSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now.Add(time.Second),
			secondTokenHash,
		)

	store := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	snapshotAt := fixture.now.Add(
		time.Minute,
	)

	target, found, err :=
		store.FindAllSessionRevocationTargetsByRefreshTokenHash(
			fixture.ctx,
			firstTokenHash,
			snapshotAt,
		)
	if err != nil {
		t.Fatalf(
			"FindAllSessionRevocationTargetsByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"expected all sessions revocation target to be found",
		)
	}

	if target.IdentityID != fixture.identityID {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			target.IdentityID,
			fixture.identityID,
		)
	}

	if len(target.Sessions) != 2 {
		t.Fatalf(
			"snapshot session count = %d, expected 2",
			len(target.Sessions),
		)
	}

	snapshotSessionIDs := make(
		[]string,
		0,
		len(target.Sessions),
	)

	snapshotSessionSet := make(
		map[string]bool,
		len(target.Sessions),
	)

	for _, session := range target.Sessions {
		snapshotSessionIDs = append(
			snapshotSessionIDs,
			session.SessionID,
		)

		snapshotSessionSet[session.SessionID] = true
	}

	if !snapshotSessionSet[firstSessionID] {
		t.Fatal(
			"first session is missing from revocation snapshot",
		)
	}

	if !snapshotSessionSet[secondSessionID] {
		t.Fatal(
			"second session is missing from revocation snapshot",
		)
	}

	thirdSessionCreatedAt := snapshotAt.Add(
		time.Minute,
	)

	thirdSessionID :=
		fixture.createSessionWithRefreshToken(
			thirdSessionCreatedAt,
			thirdTokenHash,
		)

	revokeAt := thirdSessionCreatedAt.Add(
		time.Minute,
	)

	err = store.RevokeSessions(
		fixture.ctx,
		target.IdentityID,
		snapshotSessionIDs,
		revokeAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSessions() returned an error: %v",
			err,
		)
	}

	firstState := fixture.readRevocationState(
		firstSessionID,
		firstTokenHash,
	)

	secondState := fixture.readRevocationState(
		secondSessionID,
		secondTokenHash,
	)

	thirdState := fixture.readRevocationState(
		thirdSessionID,
		thirdTokenHash,
	)

	if firstState.sessionRevokedAt == nil ||
		firstState.tokenRevokedAt == nil {
		t.Fatal(
			"first snapshot session and refresh token were not revoked",
		)
	}

	if secondState.sessionRevokedAt == nil ||
		secondState.tokenRevokedAt == nil {
		t.Fatal(
			"second snapshot session and refresh token were not revoked",
		)
	}

	if thirdState.sessionRevokedAt != nil {
		t.Fatal(
			"session created after snapshot was revoked",
		)
	}

	if thirdState.tokenRevokedAt != nil {
		t.Fatal(
			"refresh token created after snapshot was revoked",
		)
	}
	assertIdentitySessionsRevokedOutboxEvent(
		t,
		fixture,
		[]string{
			firstSessionID,
			secondSessionID,
		},
		revokeAt,
	)

	eventCount := countIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"sessions revoked outbox event count = %d, want 1",
			eventCount,
		)
	}

	err = store.RevokeSessions(
		fixture.ctx,
		target.IdentityID,
		snapshotSessionIDs,
		revokeAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"second RevokeSessions() returned an error: %v",
			err,
		)
	}

	eventCount = countIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"sessions revoked outbox event count after repeated revocation = %d, want 1",
			eventCount,
		)
	}
}

func TestAllSessionsRevocationStoreRevokeSessionCreatesOutboxEvent(
	t *testing.T,
) {
	fixture := newAllSessionsRevocationTestFixture(
		t,
		"+9647500000154",
	)

	tokenHash := strings.Repeat(
		"d",
		64,
	)

	sessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now,
			tokenHash,
		)

	cleanupIdentitySessionRevokedOutboxEvents(
		t,
		fixture,
	)

	store := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	revokedAt := fixture.now.Add(
		time.Minute,
	)

	err := store.RevokeSession(
		fixture.ctx,
		fixture.identityID,
		sessionID,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSession() returned an error: %v",
			err,
		)
	}

	state := fixture.readRevocationState(
		sessionID,
		tokenHash,
	)

	if state.sessionRevokedAt == nil {
		t.Fatal(
			"authentication session was not revoked",
		)
	}

	if state.tokenRevokedAt == nil {
		t.Fatal(
			"session refresh token was not revoked",
		)
	}

	if !state.sessionRevokedAt.Equal(revokedAt) {
		t.Fatalf(
			"session revoked at = %v, want %v",
			state.sessionRevokedAt,
			revokedAt,
		)
	}

	if !state.tokenRevokedAt.Equal(revokedAt) {
		t.Fatalf(
			"refresh token revoked at = %v, want %v",
			state.tokenRevokedAt,
			revokedAt,
		)
	}

	assertIdentitySessionRevokedOutboxEvent(
		t,
		fixture,
		sessionID,
		revokedAt,
	)

	eventCount := countIdentitySessionRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"session revoked outbox event count = %d, want 1",
			eventCount,
		)
	}

	err = store.RevokeSession(
		fixture.ctx,
		fixture.identityID,
		sessionID,
		revokedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"second RevokeSession() returned an error: %v",
			err,
		)
	}

	eventCount = countIdentitySessionRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"session revoked outbox event count after second revocation = %d, want 1",
			eventCount,
		)
	}
}

func TestAllSessionsRevocationStoreRevokeSessionsEmitsOnlyNewlyRevokedSessions(
	t *testing.T,
) {
	fixture := newAllSessionsRevocationTestFixture(
		t,
		"+9647500000155",
	)

	cleanupIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	firstTokenHash := strings.Repeat(
		"e",
		64,
	)

	secondTokenHash := strings.Repeat(
		"f",
		64,
	)

	firstSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now,
			firstTokenHash,
		)

	secondSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now.Add(time.Second),
			secondTokenHash,
		)

	concurrentRevokedAt := fixture.now.Add(
		time.Minute,
	)

	_, err := fixture.pool.Exec(
		fixture.ctx,
		`
			UPDATE auth_sessions
			SET
				revoked_at = $1,
				updated_at = $1
			WHERE identity_id = $2::uuid
			  AND id = $3::uuid
			  AND revoked_at IS NULL
		`,
		concurrentRevokedAt,
		fixture.identityID,
		firstSessionID,
	)
	if err != nil {
		t.Fatalf(
			"simulate concurrent session revocation: %v",
			err,
		)
	}

	firstStateBeforeBulk := fixture.readRevocationState(
		firstSessionID,
		firstTokenHash,
	)

	if firstStateBeforeBulk.sessionRevokedAt == nil {
		t.Fatal(
			"first session was not revoked by concurrent operation",
		)
	}

	if firstStateBeforeBulk.tokenRevokedAt != nil {
		t.Fatal(
			"first refresh token was unexpectedly revoked before bulk operation",
		)
	}

	store := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	bulkRevokedAt := concurrentRevokedAt.Add(
		time.Minute,
	)

	err = store.RevokeSessions(
		fixture.ctx,
		fixture.identityID,
		[]string{
			firstSessionID,
			secondSessionID,
		},
		bulkRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSessions() returned an error: %v",
			err,
		)
	}

	firstState := fixture.readRevocationState(
		firstSessionID,
		firstTokenHash,
	)

	secondState := fixture.readRevocationState(
		secondSessionID,
		secondTokenHash,
	)

	if firstState.sessionRevokedAt == nil {
		t.Fatal(
			"first session is not revoked",
		)
	}

	if !firstState.sessionRevokedAt.Equal(
		concurrentRevokedAt,
	) {
		t.Fatalf(
			"first session revoked at = %v, want original concurrent time %v",
			firstState.sessionRevokedAt,
			concurrentRevokedAt,
		)
	}

	if firstState.tokenRevokedAt == nil {
		t.Fatal(
			"first refresh token was not revoked by bulk operation",
		)
	}

	if !firstState.tokenRevokedAt.Equal(
		bulkRevokedAt,
	) {
		t.Fatalf(
			"first refresh token revoked at = %v, want %v",
			firstState.tokenRevokedAt,
			bulkRevokedAt,
		)
	}

	if secondState.sessionRevokedAt == nil {
		t.Fatal(
			"second session was not revoked",
		)
	}

	if !secondState.sessionRevokedAt.Equal(
		bulkRevokedAt,
	) {
		t.Fatalf(
			"second session revoked at = %v, want %v",
			secondState.sessionRevokedAt,
			bulkRevokedAt,
		)
	}

	if secondState.tokenRevokedAt == nil {
		t.Fatal(
			"second refresh token was not revoked",
		)
	}

	if !secondState.tokenRevokedAt.Equal(
		bulkRevokedAt,
	) {
		t.Fatalf(
			"second refresh token revoked at = %v, want %v",
			secondState.tokenRevokedAt,
			bulkRevokedAt,
		)
	}

	assertIdentitySessionsRevokedOutboxEvent(
		t,
		fixture,
		[]string{
			secondSessionID,
		},
		bulkRevokedAt,
	)

	eventCount := countIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"sessions revoked outbox event count = %d, want 1",
			eventCount,
		)
	}
}

func assertIdentitySessionRevokedOutboxEvent(
	t *testing.T,
	fixture *allSessionsRevocationTestFixture,
	sessionID string,
	occurredAt time.Time,
) {
	t.Helper()

	var aggregateType string
	var aggregateID string
	var eventType string
	var schemaVersion int16
	var payloadIdentityID string
	var payloadSessionID string
	var storedOccurredAt time.Time
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
		string(auth.IdentityDomainEventSessionRevoked),
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
			"query identity session revoked outbox event: %v",
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

	if aggregateID != fixture.identityID {
		t.Fatalf(
			"aggregate ID = %q, want %q",
			aggregateID,
			fixture.identityID,
		)
	}

	if eventType != string(
		auth.IdentityDomainEventSessionRevoked,
	) {
		t.Fatalf(
			"event type = %q, want %q",
			eventType,
			auth.IdentityDomainEventSessionRevoked,
		)
	}

	if schemaVersion !=
		auth.IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			schemaVersion,
			auth.IdentityDomainEventSchemaVersion,
		)
	}

	if payloadIdentityID != fixture.identityID {
		t.Fatalf(
			"payload identity ID = %q, want %q",
			payloadIdentityID,
			fixture.identityID,
		)
	}

	if payloadSessionID != sessionID {
		t.Fatalf(
			"payload session ID = %q, want %q",
			payloadSessionID,
			sessionID,
		)
	}

	if !storedOccurredAt.Equal(occurredAt) {
		t.Fatalf(
			"occurred at = %v, want %v",
			storedOccurredAt,
			occurredAt,
		)
	}

	if published {
		t.Fatal(
			"identity session revoked outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, want 0",
			publishAttempts,
		)
	}
}

func countIdentitySessionRevokedOutboxEvents(
	t *testing.T,
	fixture *allSessionsRevocationTestFixture,
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
		string(auth.IdentityDomainEventSessionRevoked),
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identity session revoked outbox events: %v",
			err,
		)
	}

	return count
}

func cleanupIdentitySessionRevokedOutboxEvents(
	t *testing.T,
	fixture *allSessionsRevocationTestFixture,
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
			fixture.identityID,
			string(auth.IdentityDomainEventSessionRevoked),
		)
		if err != nil {
			t.Errorf(
				"clean identity session revoked outbox events: %v",
				err,
			)
		}
	})
}
func assertIdentitySessionsRevokedOutboxEvent(
	t *testing.T,
	fixture *allSessionsRevocationTestFixture,
	expectedSessionIDs []string,
	occurredAt time.Time,
) {
	t.Helper()

	var aggregateType string
	var aggregateID string
	var eventType string
	var schemaVersion int16
	var payloadIdentityID string
	var sessionCount int
	var storedOccurredAt time.Time
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
				jsonb_array_length(payload -> 'session_ids'),
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
		string(auth.IdentityDomainEventSessionsRevoked),
	).Scan(
		&aggregateType,
		&aggregateID,
		&eventType,
		&schemaVersion,
		&payloadIdentityID,
		&sessionCount,
		&storedOccurredAt,
		&published,
		&publishAttempts,
	)
	if err != nil {
		t.Fatalf(
			"query identity sessions revoked outbox event: %v",
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

	if aggregateID != fixture.identityID {
		t.Fatalf(
			"aggregate ID = %q, want %q",
			aggregateID,
			fixture.identityID,
		)
	}

	if eventType != string(
		auth.IdentityDomainEventSessionsRevoked,
	) {
		t.Fatalf(
			"event type = %q, want %q",
			eventType,
			auth.IdentityDomainEventSessionsRevoked,
		)
	}

	if schemaVersion != auth.IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			schemaVersion,
			auth.IdentityDomainEventSchemaVersion,
		)
	}

	if payloadIdentityID != fixture.identityID {
		t.Fatalf(
			"payload identity ID = %q, want %q",
			payloadIdentityID,
			fixture.identityID,
		)
	}

	if sessionCount != len(expectedSessionIDs) {
		t.Fatalf(
			"payload session count = %d, want %d",
			sessionCount,
			len(expectedSessionIDs),
		)
	}

	for _, sessionID := range expectedSessionIDs {
		var exists bool

		err := fixture.pool.QueryRow(
			fixture.ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM outbox_events
					WHERE aggregate_type = $1
					  AND aggregate_id = $2::uuid
					  AND event_type = $3
					  AND payload -> 'session_ids' @> jsonb_build_array($4::text)
				)
			`,
			identityOutboxAggregateType,
			fixture.identityID,
			string(auth.IdentityDomainEventSessionsRevoked),
			sessionID,
		).Scan(
			&exists,
		)
		if err != nil {
			t.Fatalf(
				"query session ID %q in sessions revoked event: %v",
				sessionID,
				err,
			)
		}

		if !exists {
			t.Fatalf(
				"session ID %q is missing from sessions revoked event payload",
				sessionID,
			)
		}
	}

	if !storedOccurredAt.Equal(occurredAt) {
		t.Fatalf(
			"occurred at = %v, want %v",
			storedOccurredAt,
			occurredAt,
		)
	}

	if published {
		t.Fatal(
			"identity sessions revoked outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, want 0",
			publishAttempts,
		)
	}
}

func countIdentitySessionsRevokedOutboxEvents(
	t *testing.T,
	fixture *allSessionsRevocationTestFixture,
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
		string(auth.IdentityDomainEventSessionsRevoked),
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identity sessions revoked outbox events: %v",
			err,
		)
	}

	return count
}

func cleanupIdentitySessionsRevokedOutboxEvents(
	t *testing.T,
	fixture *allSessionsRevocationTestFixture,
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
			fixture.identityID,
			string(auth.IdentityDomainEventSessionsRevoked),
		)
		if err != nil {
			t.Errorf(
				"clean identity sessions revoked outbox events: %v",
				err,
			)
		}
	})
}
