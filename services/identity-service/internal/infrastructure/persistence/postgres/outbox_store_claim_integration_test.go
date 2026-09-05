//go:build integration

package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outboxClaimIntegrationFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
}

func outboxClaimIntegrationReferenceTime() time.Time {
	return time.Date(
		2000,
		time.January,
		1,
		12,
		0,
		0,
		0,
		time.UTC,
	)
}

func newOutboxClaimIntegrationFixture(
	t *testing.T,
) *outboxClaimIntegrationFixture {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal(
			"DATABASE_URL is required for integration test",
		)
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

	return &outboxClaimIntegrationFixture{
		t:    t,
		ctx:  ctx,
		pool: pool,
	}
}

func (f *outboxClaimIntegrationFixture) createEvent(
	aggregateID string,
	eventType string,
	occurredAt time.Time,
	availableAt time.Time,
) string {
	f.t.Helper()

	var eventID string

	err := f.pool.QueryRow(
		f.ctx,
		`
			INSERT INTO outbox_events (
				aggregate_type,
				aggregate_id,
				event_type,
				schema_version,
				payload,
				occurred_at,
				available_at
			)
			VALUES (
				$1,
				$2::uuid,
				$3,
				$4,
				$5::jsonb,
				$6,
				$7
			)
			RETURNING id::text
		`,
		"identity",
		aggregateID,
		eventType,
		int16(1),
		fmt.Sprintf(
			`{"identity_id":%q}`,
			aggregateID,
		),
		occurredAt.UTC(),
		availableAt.UTC(),
	).Scan(
		&eventID,
	)
	if err != nil {
		f.t.Fatalf(
			"create outbox claim test event: %v",
			err,
		)
	}

	f.t.Cleanup(func() {
		_, cleanupErr := f.pool.Exec(
			context.Background(),
			`
				DELETE FROM outbox_events
				WHERE id = $1::uuid
			`,
			eventID,
		)
		if cleanupErr != nil {
			f.t.Errorf(
				"clean outbox claim test event: %v",
				cleanupErr,
			)
		}
	})

	return eventID
}

func (f *outboxClaimIntegrationFixture) claimState(
	eventID string,
) (
	string,
	time.Time,
	time.Time,
	int,
) {
	f.t.Helper()

	var claimToken string
	var claimedAt time.Time
	var availableAt time.Time
	var publishAttempts int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT
				claim_token::text,
				claimed_at,
				available_at,
				publish_attempts
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		eventID,
	).Scan(
		&claimToken,
		&claimedAt,
		&availableAt,
		&publishAttempts,
	)
	if err != nil {
		f.t.Fatalf(
			"query outbox claim state: %v",
			err,
		)
	}

	return claimToken,
		claimedAt.UTC(),
		availableAt.UTC(),
		publishAttempts
}

func TestOutboxStoreClaimPendingClaimsEligibleEventsInOrder(
	t *testing.T,
) {
	fixture :=
		newOutboxClaimIntegrationFixture(t)

	claimedAt :=
		outboxClaimIntegrationReferenceTime()

	const firstAggregateID = "11111111-1111-4111-8111-111111111111"

	const secondAggregateID = "22222222-2222-4222-8222-222222222222"

	const futureAggregateID = "33333333-3333-4333-8333-333333333333"

	firstEventID := fixture.createEvent(
		firstAggregateID,
		"identity.suspended",
		claimedAt.Add(-2*time.Minute),
		claimedAt.Add(-2*time.Minute),
	)

	secondEventID := fixture.createEvent(
		secondAggregateID,
		"identity.disabled",
		claimedAt.Add(-time.Minute),
		claimedAt.Add(-time.Minute),
	)

	fixture.createEvent(
		futureAggregateID,
		"identity.reactivated",
		claimedAt,
		claimedAt.Add(time.Hour),
	)

	store := NewOutboxStore(
		fixture.pool,
	)

	events, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     claimedAt,
			LeaseDuration: 30 * time.Second,
			Limit:         2,
		},
	)
	if err != nil {
		t.Fatalf(
			"ClaimPending() returned an error: %v",
			err,
		)
	}

	if len(events) != 2 {
		t.Fatalf(
			"claimed events = %d, expected 2",
			len(events),
		)
	}

	if events[0].ID != firstEventID {
		t.Fatalf(
			"first claimed event ID = %q, expected %q",
			events[0].ID,
			firstEventID,
		)
	}

	if events[1].ID != secondEventID {
		t.Fatalf(
			"second claimed event ID = %q, expected %q",
			events[1].ID,
			secondEventID,
		)
	}

	for _, event := range events {
		if event.ClaimToken == "" {
			t.Fatalf(
				"claimed event %q has blank claim token",
				event.ID,
			)
		}

		if event.AggregateType != "identity" {
			t.Fatalf(
				"aggregate type = %q, expected identity",
				event.AggregateType,
			)
		}

		if len(event.Payload) == 0 {
			t.Fatalf(
				"claimed event %q has empty payload",
				event.ID,
			)
		}

		if event.SchemaVersion != 1 {
			t.Fatalf(
				"schema version = %d, expected 1",
				event.SchemaVersion,
			)
		}
	}

	claimToken,
		storedClaimedAt,
		availableAt,
		publishAttempts :=
		fixture.claimState(firstEventID)

	if claimToken != events[0].ClaimToken {
		t.Fatalf(
			"stored claim token = %q, expected %q",
			claimToken,
			events[0].ClaimToken,
		)
	}

	if !storedClaimedAt.Equal(claimedAt) {
		t.Fatalf(
			"claimed at = %v, expected %v",
			storedClaimedAt,
			claimedAt,
		)
	}

	expectedLeaseUntil :=
		claimedAt.Add(30 * time.Second)

	if !availableAt.Equal(expectedLeaseUntil) {
		t.Fatalf(
			"available at = %v, expected %v",
			availableAt,
			expectedLeaseUntil,
		)
	}

	if publishAttempts != 1 {
		t.Fatalf(
			"publish attempts = %d, expected 1",
			publishAttempts,
		)
	}
}

func TestOutboxStoreClaimPendingDoesNotReclaimBeforeLeaseExpires(
	t *testing.T,
) {
	fixture :=
		newOutboxClaimIntegrationFixture(t)

	claimedAt :=
		outboxClaimIntegrationReferenceTime()

	const aggregateID = "44444444-4444-4444-8444-444444444444"

	eventID := fixture.createEvent(
		aggregateID,
		"identity.suspended",
		claimedAt.Add(-time.Minute),
		claimedAt.Add(-time.Minute),
	)

	store := NewOutboxStore(
		fixture.pool,
	)

	firstClaim, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     claimedAt,
			LeaseDuration: time.Minute,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"first ClaimPending() returned an error: %v",
			err,
		)
	}

	if len(firstClaim) != 1 {
		t.Fatalf(
			"first claim returned %d events, expected 1",
			len(firstClaim),
		)
	}

	if firstClaim[0].ID != eventID {
		t.Fatalf(
			"first claimed event ID = %q, expected %q",
			firstClaim[0].ID,
			eventID,
		)
	}

	secondClaim, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt: claimedAt.Add(
				30 * time.Second,
			),
			LeaseDuration: time.Minute,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"second ClaimPending() returned an error: %v",
			err,
		)
	}

	if len(secondClaim) != 0 {
		t.Fatalf(
			"event was reclaimed before lease expiration",
		)
	}
}

func TestOutboxStoreClaimPendingReclaimsExpiredLease(
	t *testing.T,
) {
	fixture :=
		newOutboxClaimIntegrationFixture(t)

	claimedAt :=
		outboxClaimIntegrationReferenceTime()

	const aggregateID = "55555555-5555-4555-8555-555555555555"

	eventID := fixture.createEvent(
		aggregateID,
		"identity.disabled",
		claimedAt.Add(-time.Minute),
		claimedAt.Add(-time.Minute),
	)

	store := NewOutboxStore(
		fixture.pool,
	)

	firstClaim, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     claimedAt,
			LeaseDuration: 30 * time.Second,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"first ClaimPending() returned an error: %v",
			err,
		)
	}

	if len(firstClaim) != 1 {
		t.Fatalf(
			"first claim returned %d events, expected 1",
			len(firstClaim),
		)
	}

	reclaimedAt :=
		claimedAt.Add(31 * time.Second)

	secondClaim, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     reclaimedAt,
			LeaseDuration: time.Minute,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"second ClaimPending() returned an error: %v",
			err,
		)
	}

	if len(secondClaim) != 1 {
		t.Fatalf(
			"expired lease claim returned %d events, expected 1",
			len(secondClaim),
		)
	}

	if secondClaim[0].ID != eventID {
		t.Fatalf(
			"reclaimed event ID = %q, expected %q",
			secondClaim[0].ID,
			eventID,
		)
	}

	if secondClaim[0].ClaimToken ==
		firstClaim[0].ClaimToken {
		t.Fatal(
			"reclaimed event reused previous claim token",
		)
	}

	claimToken,
		storedClaimedAt,
		_,
		publishAttempts :=
		fixture.claimState(eventID)

	if claimToken != secondClaim[0].ClaimToken {
		t.Fatalf(
			"stored claim token = %q, expected %q",
			claimToken,
			secondClaim[0].ClaimToken,
		)
	}

	if !storedClaimedAt.Equal(reclaimedAt) {
		t.Fatalf(
			"reclaimed at = %v, expected %v",
			storedClaimedAt,
			reclaimedAt,
		)
	}

	if publishAttempts != 2 {
		t.Fatalf(
			"publish attempts = %d, expected 2",
			publishAttempts,
		)
	}
}

func TestOutboxStoreClaimPendingConcurrentWorkersDoNotDuplicateClaims(
	t *testing.T,
) {
	fixture :=
		newOutboxClaimIntegrationFixture(t)

	claimedAt :=
		outboxClaimIntegrationReferenceTime()

	const eventCount = 20

	expectedIDs := make(
		map[string]struct{},
		eventCount,
	)

	for index := 0; index < eventCount; index++ {
		aggregateID := fmt.Sprintf(
			"aaaaaaaa-aaaa-4aaa-8aaa-%012d",
			index+1,
		)

		eventID := fixture.createEvent(
			aggregateID,
			"identity.suspended",
			claimedAt.Add(
				time.Duration(index)*time.Millisecond,
			),
			claimedAt.Add(-time.Second),
		)

		expectedIDs[eventID] = struct{}{}
	}

	store := NewOutboxStore(
		fixture.pool,
	)

	start := make(chan struct{})

	type claimResult struct {
		events []outboxapp.Event
		err    error
	}

	results := make(
		chan claimResult,
		2,
	)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	claim := func() {
		defer waitGroup.Done()

		<-start

		events, err := store.ClaimPending(
			fixture.ctx,
			outboxapp.ClaimPendingInput{
				ClaimedAt:     claimedAt,
				LeaseDuration: time.Minute,
				Limit:         eventCount,
			},
		)

		results <- claimResult{
			events: events,
			err:    err,
		}
	}

	go claim()
	go claim()

	close(start)

	waitGroup.Wait()
	close(results)

	claimedIDs := make(
		map[string]struct{},
		eventCount,
	)

	totalClaimed := 0

	for result := range results {
		if result.err != nil {
			t.Fatalf(
				"concurrent ClaimPending() returned an error: %v",
				result.err,
			)
		}

		for _, event := range result.events {
			totalClaimed++

			if _, duplicate :=
				claimedIDs[event.ID]; duplicate {
				t.Fatalf(
					"event %q was claimed by multiple workers",
					event.ID,
				)
			}

			claimedIDs[event.ID] = struct{}{}
		}
	}

	if totalClaimed != eventCount {
		t.Fatalf(
			"concurrent workers claimed %d events, expected %d",
			totalClaimed,
			eventCount,
		)
	}

	for eventID := range expectedIDs {
		if _, found := claimedIDs[eventID]; !found {
			t.Fatalf(
				"event %q was not claimed",
				eventID,
			)
		}
	}
}

func TestOutboxStoreClaimPendingRejectsInvalidInput(
	t *testing.T,
) {
	fixture :=
		newOutboxClaimIntegrationFixture(t)

	store := NewOutboxStore(
		fixture.pool,
	)

	tests := []struct {
		name  string
		input outboxapp.ClaimPendingInput
	}{
		{
			name: "zero claim time",
			input: outboxapp.ClaimPendingInput{
				ClaimedAt:     time.Time{},
				LeaseDuration: time.Minute,
				Limit:         1,
			},
		},
		{
			name: "zero lease duration",
			input: outboxapp.ClaimPendingInput{
				ClaimedAt:     time.Now(),
				LeaseDuration: 0,
				Limit:         1,
			},
		},
		{
			name: "negative lease duration",
			input: outboxapp.ClaimPendingInput{
				ClaimedAt:     time.Now(),
				LeaseDuration: -time.Second,
				Limit:         1,
			},
		},
		{
			name: "zero limit",
			input: outboxapp.ClaimPendingInput{
				ClaimedAt:     time.Now(),
				LeaseDuration: time.Minute,
				Limit:         0,
			},
		},
		{
			name: "limit above maximum",
			input: outboxapp.ClaimPendingInput{
				ClaimedAt:     time.Now(),
				LeaseDuration: time.Minute,
				Limit:         maxOutboxClaimBatchSize + 1,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				events, err := store.ClaimPending(
					fixture.ctx,
					testCase.input,
				)

				if err == nil {
					t.Fatal(
						"ClaimPending() accepted invalid input",
					)
				}

				if events != nil {
					t.Fatalf(
						"ClaimPending() events = %+v, expected nil",
						events,
					)
				}
			},
		)
	}
}
