//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identityLifecycleIntegrationFixture struct {
	t          *testing.T
	ctx        context.Context
	pool       *pgxpool.Pool
	identityID string
}

type identityLifecycleOutboxEvent struct {
	aggregateType   string
	aggregateID     string
	eventType       string
	schemaVersion   int16
	payloadIdentity string
	previousStatus  string
	currentStatus   string
	occurredAt      time.Time
	availableAt     time.Time
	published       bool
	publishAttempts int
}

func newIdentityLifecycleIntegrationFixture(
	t *testing.T,
	status auth.IdentityStatus,
) *identityLifecycleIntegrationFixture {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				status
			)
			VALUES ($1)
			RETURNING id::text
		`,
		string(status),
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create lifecycle test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM outbox_events
				WHERE aggregate_type = $1
				  AND aggregate_id = $2::uuid
			`,
			identityOutboxAggregateType,
			identityID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean lifecycle outbox events: %v",
				cleanupErr,
			)
		}

		_, cleanupErr = pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1::uuid
			`,
			identityID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean lifecycle test identity: %v",
				cleanupErr,
			)
		}
	})

	return &identityLifecycleIntegrationFixture{
		t:          t,
		ctx:        ctx,
		pool:       pool,
		identityID: identityID,
	}
}

func (f *identityLifecycleIntegrationFixture) databaseNow() time.Time {
	f.t.Helper()

	var now time.Time

	err := f.pool.QueryRow(
		f.ctx,
		"SELECT CURRENT_TIMESTAMP",
	).Scan(
		&now,
	)
	if err != nil {
		f.t.Fatalf(
			"query database time: %v",
			err,
		)
	}

	return now.UTC()
}

func (f *identityLifecycleIntegrationFixture) createActiveSession() string {
	f.t.Helper()

	baseTime := f.databaseNow()

	var sessionID string

	err := f.pool.QueryRow(
		f.ctx,
		`
			INSERT INTO auth_sessions (
				id,
				identity_id,
				expires_at
			)
			VALUES (
				gen_random_uuid(),
				$1::uuid,
				$2
			)
			RETURNING id::text
		`,
		f.identityID,
		baseTime.Add(time.Hour),
	).Scan(
		&sessionID,
	)
	if err != nil {
		f.t.Fatalf(
			"create lifecycle test session: %v",
			err,
		)
	}

	refreshTokenHash := fmt.Sprintf(
		"%x",
		sha256.Sum256([]byte("lifecycle-refresh-"+sessionID)),
	)

	_, err = f.pool.Exec(
		f.ctx,
		`
			INSERT INTO refresh_tokens (
				session_id,
				token_hash,
				expires_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3
			)
		`,
		sessionID,
		refreshTokenHash,
		baseTime.Add(30*time.Minute),
	)
	if err != nil {
		f.t.Fatalf(
			"create lifecycle test refresh token: %v",
			err,
		)
	}

	return sessionID
}

func (f *identityLifecycleIntegrationFixture) identityStatus() auth.IdentityStatus {
	f.t.Helper()

	var statusValue string

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT status
			FROM identities
			WHERE id = $1::uuid
		`,
		f.identityID,
	).Scan(
		&statusValue,
	)
	if err != nil {
		f.t.Fatalf(
			"query lifecycle identity status: %v",
			err,
		)
	}

	status, err := auth.ParseIdentityStatus(
		statusValue,
	)
	if err != nil {
		f.t.Fatalf(
			"parse lifecycle identity status: %v",
			err,
		)
	}

	return status
}

func (f *identityLifecycleIntegrationFixture) sessionRevoked(
	sessionID string,
) bool {
	f.t.Helper()

	var revoked bool

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT revoked_at IS NOT NULL
			FROM auth_sessions
			WHERE id = $1::uuid
			  AND identity_id = $2::uuid
		`,
		sessionID,
		f.identityID,
	).Scan(
		&revoked,
	)
	if err != nil {
		f.t.Fatalf(
			"query lifecycle session revocation: %v",
			err,
		)
	}

	return revoked
}

func (f *identityLifecycleIntegrationFixture) refreshTokenRevoked(
	sessionID string,
) bool {
	f.t.Helper()

	var revoked bool

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT revoked_at IS NOT NULL
			FROM refresh_tokens
			WHERE session_id = $1::uuid
		`,
		sessionID,
	).Scan(
		&revoked,
	)
	if err != nil {
		f.t.Fatalf(
			"query lifecycle refresh token revocation: %v",
			err,
		)
	}

	return revoked
}

func (f *identityLifecycleIntegrationFixture) outboxEventsForIdentity(
	identityID string,
) []identityLifecycleOutboxEvent {
	f.t.Helper()

	rows, err := f.pool.Query(
		f.ctx,
		`
			SELECT
				aggregate_type,
				aggregate_id::text,
				event_type,
				schema_version,
				payload ->> 'identity_id',
				payload ->> 'previous_status',
				payload ->> 'current_status',
				occurred_at,
				available_at,
				published_at IS NOT NULL,
				publish_attempts
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			ORDER BY
				occurred_at ASC,
				created_at ASC,
				id ASC
		`,
		identityOutboxAggregateType,
		identityID,
	)
	if err != nil {
		f.t.Fatalf(
			"query lifecycle outbox events: %v",
			err,
		)
	}
	defer rows.Close()

	var events []identityLifecycleOutboxEvent

	for rows.Next() {
		var event identityLifecycleOutboxEvent

		if err := rows.Scan(
			&event.aggregateType,
			&event.aggregateID,
			&event.eventType,
			&event.schemaVersion,
			&event.payloadIdentity,
			&event.previousStatus,
			&event.currentStatus,
			&event.occurredAt,
			&event.availableAt,
			&event.published,
			&event.publishAttempts,
		); err != nil {
			f.t.Fatalf(
				"scan lifecycle outbox event: %v",
				err,
			)
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		f.t.Fatalf(
			"iterate lifecycle outbox events: %v",
			err,
		)
	}

	return events
}

func (f *identityLifecycleIntegrationFixture) assertLifecycleOutboxEvent(
	event identityLifecycleOutboxEvent,
	eventType auth.IdentityDomainEventType,
	previousStatus auth.IdentityStatus,
	currentStatus auth.IdentityStatus,
	occurredAt time.Time,
) {
	f.t.Helper()

	if event.aggregateType != identityOutboxAggregateType {
		f.t.Fatalf(
			"outbox aggregate type = %q, expected %q",
			event.aggregateType,
			identityOutboxAggregateType,
		)
	}

	if event.aggregateID != f.identityID {
		f.t.Fatalf(
			"outbox aggregate ID = %q, expected %q",
			event.aggregateID,
			f.identityID,
		)
	}

	if event.eventType != string(eventType) {
		f.t.Fatalf(
			"outbox event type = %q, expected %q",
			event.eventType,
			eventType,
		)
	}

	if event.schemaVersion != auth.IdentityDomainEventSchemaVersion {
		f.t.Fatalf(
			"outbox schema version = %d, expected %d",
			event.schemaVersion,
			auth.IdentityDomainEventSchemaVersion,
		)
	}

	if event.payloadIdentity != f.identityID {
		f.t.Fatalf(
			"outbox payload identity ID = %q, expected %q",
			event.payloadIdentity,
			f.identityID,
		)
	}

	if event.previousStatus != string(previousStatus) {
		f.t.Fatalf(
			"outbox previous status = %q, expected %q",
			event.previousStatus,
			previousStatus,
		)
	}

	if event.currentStatus != string(currentStatus) {
		f.t.Fatalf(
			"outbox current status = %q, expected %q",
			event.currentStatus,
			currentStatus,
		)
	}

	if !event.occurredAt.Equal(occurredAt.UTC()) {
		f.t.Fatalf(
			"outbox occurred at = %v, expected %v",
			event.occurredAt,
			occurredAt.UTC(),
		)
	}

	if !event.availableAt.Equal(event.occurredAt) {
		f.t.Fatalf(
			"outbox available at = %v, expected %v",
			event.availableAt,
			event.occurredAt,
		)
	}

	if event.published {
		f.t.Fatal(
			"new lifecycle outbox event was unexpectedly marked published",
		)
	}

	if event.publishAttempts != 0 {
		f.t.Fatalf(
			"new lifecycle outbox publish attempts = %d, expected 0",
			event.publishAttempts,
		)
	}
}

func TestIdentityLifecycleStoreDeactivatesIdentityAndRevokesSessions(
	t *testing.T,
) {
	testCases := []struct {
		name         string
		targetStatus auth.IdentityStatus
		eventType    auth.IdentityDomainEventType
	}{
		{
			name:         "suspend",
			targetStatus: auth.IdentityStatusSuspended,
			eventType:    auth.IdentityDomainEventSuspended,
		},
		{
			name:         "disable",
			targetStatus: auth.IdentityStatusDisabled,
			eventType:    auth.IdentityDomainEventDisabled,
		},
	}

	for _, testCase := range testCases {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				fixture := newIdentityLifecycleIntegrationFixture(
					t,
					auth.IdentityStatusActive,
				)

				firstSessionID := fixture.createActiveSession()
				secondSessionID := fixture.createActiveSession()

				store := NewIdentityLifecycleStore(
					fixture.pool,
				)

				transitionedAt := fixture.databaseNow()

				result, found, err := store.Transition(
					fixture.ctx,
					auth.IdentityLifecycleTransition{
						IdentityID:     fixture.identityID,
						TargetStatus:   testCase.targetStatus,
						TransitionedAt: transitionedAt,
					},
				)
				if err != nil {
					t.Fatalf(
						"Transition() returned an error: %v",
						err,
					)
				}

				if !found {
					t.Fatal(
						"Transition() did not find identity",
					)
				}

				if result.PreviousStatus != auth.IdentityStatusActive {
					t.Fatalf(
						"PreviousStatus = %q, expected %q",
						result.PreviousStatus,
						auth.IdentityStatusActive,
					)
				}

				if result.CurrentStatus != testCase.targetStatus {
					t.Fatalf(
						"CurrentStatus = %q, expected %q",
						result.CurrentStatus,
						testCase.targetStatus,
					)
				}

				if !result.Changed {
					t.Fatal(
						"Transition() reported unchanged lifecycle status",
					)
				}

				if fixture.identityStatus() != testCase.targetStatus {
					t.Fatalf(
						"stored identity status = %q, expected %q",
						fixture.identityStatus(),
						testCase.targetStatus,
					)
				}

				for _, sessionID := range []string{
					firstSessionID,
					secondSessionID,
				} {
					if !fixture.sessionRevoked(sessionID) {
						t.Fatalf(
							"session %q remained active after identity deactivation",
							sessionID,
						)
					}

					if !fixture.refreshTokenRevoked(sessionID) {
						t.Fatalf(
							"refresh token for session %q remained active after identity deactivation",
							sessionID,
						)
					}
				}

				events := fixture.outboxEventsForIdentity(
					fixture.identityID,
				)

				if len(events) != 1 {
					t.Fatalf(
						"lifecycle outbox events = %d, expected 1",
						len(events),
					)
				}

				fixture.assertLifecycleOutboxEvent(
					events[0],
					testCase.eventType,
					auth.IdentityStatusActive,
					testCase.targetStatus,
					transitionedAt,
				)
			},
		)
	}
}

func TestIdentityLifecycleStoreReactivationDoesNotRestoreOldSessions(
	t *testing.T,
) {
	fixture := newIdentityLifecycleIntegrationFixture(
		t,
		auth.IdentityStatusActive,
	)

	sessionID := fixture.createActiveSession()

	store := NewIdentityLifecycleStore(
		fixture.pool,
	)

	suspendedAt := fixture.databaseNow()

	_, found, err := store.Transition(
		fixture.ctx,
		auth.IdentityLifecycleTransition{
			IdentityID:     fixture.identityID,
			TargetStatus:   auth.IdentityStatusSuspended,
			TransitionedAt: suspendedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"suspend identity: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"suspend transition did not find identity",
		)
	}

	if !fixture.sessionRevoked(sessionID) {
		t.Fatal(
			"session was not revoked during suspension",
		)
	}

	if !fixture.refreshTokenRevoked(sessionID) {
		t.Fatal(
			"refresh token was not revoked during suspension",
		)
	}

	reactivatedAt := fixture.databaseNow()

	result, found, err := store.Transition(
		fixture.ctx,
		auth.IdentityLifecycleTransition{
			IdentityID:     fixture.identityID,
			TargetStatus:   auth.IdentityStatusActive,
			TransitionedAt: reactivatedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"reactivate identity: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"reactivation did not find identity",
		)
	}

	if result.PreviousStatus != auth.IdentityStatusSuspended {
		t.Fatalf(
			"PreviousStatus = %q, expected %q",
			result.PreviousStatus,
			auth.IdentityStatusSuspended,
		)
	}

	if result.CurrentStatus != auth.IdentityStatusActive {
		t.Fatalf(
			"CurrentStatus = %q, expected %q",
			result.CurrentStatus,
			auth.IdentityStatusActive,
		)
	}

	if !result.Changed {
		t.Fatal(
			"reactivation was unexpectedly reported as unchanged",
		)
	}

	if fixture.identityStatus() != auth.IdentityStatusActive {
		t.Fatalf(
			"stored identity status = %q, expected %q",
			fixture.identityStatus(),
			auth.IdentityStatusActive,
		)
	}

	if !fixture.sessionRevoked(sessionID) {
		t.Fatal(
			"reactivation restored an old authentication session",
		)
	}

	if !fixture.refreshTokenRevoked(sessionID) {
		t.Fatal(
			"reactivation restored an old refresh token",
		)
	}

	events := fixture.outboxEventsForIdentity(
		fixture.identityID,
	)

	if len(events) != 2 {
		t.Fatalf(
			"lifecycle outbox events = %d, expected 2",
			len(events),
		)
	}

	var suspendedEvent *identityLifecycleOutboxEvent
	var reactivatedEvent *identityLifecycleOutboxEvent

	for index := range events {
		switch events[index].eventType {
		case string(auth.IdentityDomainEventSuspended):
			suspendedEvent = &events[index]

		case string(auth.IdentityDomainEventReactivated):
			reactivatedEvent = &events[index]
		}
	}

	if suspendedEvent == nil {
		t.Fatal(
			"identity.suspended outbox event was not created",
		)
	}

	if reactivatedEvent == nil {
		t.Fatal(
			"identity.reactivated outbox event was not created",
		)
	}

	fixture.assertLifecycleOutboxEvent(
		*suspendedEvent,
		auth.IdentityDomainEventSuspended,
		auth.IdentityStatusActive,
		auth.IdentityStatusSuspended,
		suspendedAt,
	)

	fixture.assertLifecycleOutboxEvent(
		*reactivatedEvent,
		auth.IdentityDomainEventReactivated,
		auth.IdentityStatusSuspended,
		auth.IdentityStatusActive,
		reactivatedAt,
	)
}

func TestIdentityLifecycleStoreReturnsUnchangedForSameStatus(
	t *testing.T,
) {
	fixture := newIdentityLifecycleIntegrationFixture(
		t,
		auth.IdentityStatusActive,
	)

	sessionID := fixture.createActiveSession()

	store := NewIdentityLifecycleStore(
		fixture.pool,
	)

	result, found, err := store.Transition(
		fixture.ctx,
		auth.IdentityLifecycleTransition{
			IdentityID:     fixture.identityID,
			TargetStatus:   auth.IdentityStatusActive,
			TransitionedAt: fixture.databaseNow(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Transition() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"Transition() did not find identity",
		)
	}

	if result.PreviousStatus != auth.IdentityStatusActive ||
		result.CurrentStatus != auth.IdentityStatusActive {
		t.Fatalf(
			"Transition() result = %+v, expected active to active",
			result,
		)
	}

	if result.Changed {
		t.Fatal(
			"same-status transition was unexpectedly reported as changed",
		)
	}

	if fixture.sessionRevoked(sessionID) {
		t.Fatal(
			"same active status transition unexpectedly revoked session",
		)
	}

	if fixture.refreshTokenRevoked(sessionID) {
		t.Fatal(
			"same active status transition unexpectedly revoked refresh token",
		)
	}

	events := fixture.outboxEventsForIdentity(
		fixture.identityID,
	)

	if len(events) != 0 {
		t.Fatalf(
			"same-status transition created %d outbox events, expected 0",
			len(events),
		)
	}
}

func TestIdentityLifecycleStoreReturnsNotFound(
	t *testing.T,
) {
	fixture := newIdentityLifecycleIntegrationFixture(
		t,
		auth.IdentityStatusActive,
	)

	store := NewIdentityLifecycleStore(
		fixture.pool,
	)

	const missingIdentityID = "ffffffff-ffff-4fff-8fff-ffffffffffff"

	result, found, err := store.Transition(
		fixture.ctx,
		auth.IdentityLifecycleTransition{
			IdentityID:     missingIdentityID,
			TargetStatus:   auth.IdentityStatusSuspended,
			TransitionedAt: fixture.databaseNow(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Transition() returned an error: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"Transition() unexpectedly found missing identity",
		)
	}

	if result != (auth.IdentityLifecycleTransitionResult{}) {
		t.Fatalf(
			"Transition() result = %+v, expected zero value",
			result,
		)
	}

	events := fixture.outboxEventsForIdentity(
		missingIdentityID,
	)

	if len(events) != 0 {
		t.Fatalf(
			"missing identity transition created %d outbox events, expected 0",
			len(events),
		)
	}
}
