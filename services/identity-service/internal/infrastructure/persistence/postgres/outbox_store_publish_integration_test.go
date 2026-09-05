//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outboxPublishIntegrationFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
}

func newOutboxPublishIntegrationFixture(
	t *testing.T,
) *outboxPublishIntegrationFixture {
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

	return &outboxPublishIntegrationFixture{
		t:    t,
		ctx:  ctx,
		pool: pool,
	}
}

func (f *outboxPublishIntegrationFixture) createClaimedEvent(
	claimedAt time.Time,
	leaseDuration time.Duration,
) (
	string,
	string,
) {
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
				'identity',
				gen_random_uuid(),
				'identity.suspended',
				1,
				'{}'::jsonb,
				$1,
				$1
			)
			RETURNING id::text
		`,
		claimedAt.UTC(),
	).Scan(
		&eventID,
	)
	if err != nil {
		f.t.Fatalf(
			"create publish test outbox event: %v",
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
				"clean publish test outbox event: %v",
				cleanupErr,
			)
		}
	})

	store := NewOutboxStore(
		f.pool,
	)

	events, err := store.ClaimPending(
		f.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     claimedAt,
			LeaseDuration: leaseDuration,
			Limit:         1,
		},
	)
	if err != nil {
		f.t.Fatalf(
			"claim publish test event: %v",
			err,
		)
	}

	if len(events) != 1 {
		f.t.Fatalf(
			"claimed publish test events = %d, expected 1",
			len(events),
		)
	}

	if events[0].ID != eventID {
		f.t.Fatalf(
			"claimed event ID = %q, expected %q",
			events[0].ID,
			eventID,
		)
	}

	return eventID,
		events[0].ClaimToken
}

func (f *outboxPublishIntegrationFixture) publicationState(
	eventID string,
) (
	bool,
	time.Time,
	bool,
	bool,
	string,
) {
	f.t.Helper()

	var published bool
	var publishedAt time.Time
	var hasClaimToken bool
	var hasClaimedAt bool
	var lastError *string

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT
				published_at IS NOT NULL,
				COALESCE(
					published_at,
					'epoch'::timestamptz
				),
				claim_token IS NOT NULL,
				claimed_at IS NOT NULL,
				last_error
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		eventID,
	).Scan(
		&published,
		&publishedAt,
		&hasClaimToken,
		&hasClaimedAt,
		&lastError,
	)
	if err != nil {
		f.t.Fatalf(
			"query outbox publication state: %v",
			err,
		)
	}

	lastErrorValue := ""
	if lastError != nil {
		lastErrorValue = *lastError
	}

	return published,
		publishedAt.UTC(),
		hasClaimToken,
		hasClaimedAt,
		lastErrorValue
}

func TestOutboxStoreMarkPublishedPublishesClaimedEvent(
	t *testing.T,
) {
	fixture :=
		newOutboxPublishIntegrationFixture(t)

	claimedAt := time.Date(
		2001,
		time.January,
		1,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	eventID, claimToken :=
		fixture.createClaimedEvent(
			claimedAt,
			time.Minute,
		)

	publishedAt :=
		claimedAt.Add(10 * time.Second)

	store := NewOutboxStore(
		fixture.pool,
	)

	updated, err := store.MarkPublished(
		fixture.ctx,
		outboxapp.MarkPublishedInput{
			EventID:     eventID,
			ClaimToken:  claimToken,
			PublishedAt: publishedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkPublished() returned an error: %v",
			err,
		)
	}

	if !updated {
		t.Fatal(
			"MarkPublished() did not update claimed event",
		)
	}

	published,
		storedPublishedAt,
		hasClaimToken,
		hasClaimedAt,
		lastError :=
		fixture.publicationState(eventID)

	if !published {
		t.Fatal(
			"outbox event was not marked published",
		)
	}

	if !storedPublishedAt.Equal(
		publishedAt.UTC(),
	) {
		t.Fatalf(
			"published at = %v, expected %v",
			storedPublishedAt,
			publishedAt.UTC(),
		)
	}

	if hasClaimToken {
		t.Fatal(
			"published event retained claim token",
		)
	}

	if hasClaimedAt {
		t.Fatal(
			"published event retained claimed_at",
		)
	}

	if lastError != "" {
		t.Fatalf(
			"published event last error = %q, expected empty",
			lastError,
		)
	}
}

func TestOutboxStoreMarkPublishedRejectsStaleClaimToken(
	t *testing.T,
) {
	fixture :=
		newOutboxPublishIntegrationFixture(t)

	claimedAt := time.Date(
		2001,
		time.February,
		1,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	eventID, firstClaimToken :=
		fixture.createClaimedEvent(
			claimedAt,
			30*time.Second,
		)

	store := NewOutboxStore(
		fixture.pool,
	)

	reclaimed, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     claimedAt.Add(31 * time.Second),
			LeaseDuration: time.Minute,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"reclaim event: %v",
			err,
		)
	}

	if len(reclaimed) != 1 {
		t.Fatalf(
			"reclaimed events = %d, expected 1",
			len(reclaimed),
		)
	}

	if reclaimed[0].ID != eventID {
		t.Fatalf(
			"reclaimed event ID = %q, expected %q",
			reclaimed[0].ID,
			eventID,
		)
	}

	if reclaimed[0].ClaimToken ==
		firstClaimToken {
		t.Fatal(
			"reclaim reused previous claim token",
		)
	}

	updated, err := store.MarkPublished(
		fixture.ctx,
		outboxapp.MarkPublishedInput{
			EventID:    eventID,
			ClaimToken: firstClaimToken,
			PublishedAt: claimedAt.Add(
				40 * time.Second,
			),
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkPublished() returned an error: %v",
			err,
		)
	}

	if updated {
		t.Fatal(
			"stale claim token marked event published",
		)
	}

	published,
		_,
		hasClaimToken,
		hasClaimedAt,
		_ :=
		fixture.publicationState(eventID)

	if published {
		t.Fatal(
			"stale claim token changed publication state",
		)
	}

	if !hasClaimToken {
		t.Fatal(
			"stale claim token cleared active claim token",
		)
	}

	if !hasClaimedAt {
		t.Fatal(
			"stale claim token cleared active claimed_at",
		)
	}
}

func TestOutboxStoreMarkPublishedReturnsFalseWhenAlreadyPublished(
	t *testing.T,
) {
	fixture :=
		newOutboxPublishIntegrationFixture(t)

	claimedAt := time.Date(
		2001,
		time.March,
		1,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	eventID, claimToken :=
		fixture.createClaimedEvent(
			claimedAt,
			time.Minute,
		)

	publishedAt :=
		claimedAt.Add(10 * time.Second)

	store := NewOutboxStore(
		fixture.pool,
	)

	updated, err := store.MarkPublished(
		fixture.ctx,
		outboxapp.MarkPublishedInput{
			EventID:     eventID,
			ClaimToken:  claimToken,
			PublishedAt: publishedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"first MarkPublished() returned an error: %v",
			err,
		)
	}

	if !updated {
		t.Fatal(
			"first MarkPublished() did not update event",
		)
	}

	updated, err = store.MarkPublished(
		fixture.ctx,
		outboxapp.MarkPublishedInput{
			EventID:     eventID,
			ClaimToken:  claimToken,
			PublishedAt: publishedAt.Add(time.Second),
		},
	)
	if err != nil {
		t.Fatalf(
			"second MarkPublished() returned an error: %v",
			err,
		)
	}

	if updated {
		t.Fatal(
			"already published event was updated again",
		)
	}

	published,
		storedPublishedAt,
		_,
		_,
		_ :=
		fixture.publicationState(eventID)

	if !published {
		t.Fatal(
			"event lost publication state",
		)
	}

	if !storedPublishedAt.Equal(publishedAt) {
		t.Fatalf(
			"published at changed to %v, expected %v",
			storedPublishedAt,
			publishedAt,
		)
	}
}

func TestOutboxStoreMarkPublishedReturnsFalseForMissingEvent(
	t *testing.T,
) {
	fixture :=
		newOutboxPublishIntegrationFixture(t)

	store := NewOutboxStore(
		fixture.pool,
	)

	updated, err := store.MarkPublished(
		fixture.ctx,
		outboxapp.MarkPublishedInput{
			EventID:     "ffffffff-ffff-4fff-8fff-ffffffffffff",
			ClaimToken:  "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			PublishedAt: time.Now(),
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkPublished() returned an error: %v",
			err,
		)
	}

	if updated {
		t.Fatal(
			"missing event was marked published",
		)
	}
}

func TestOutboxStoreMarkPublishedRejectsInvalidInput(
	t *testing.T,
) {
	fixture :=
		newOutboxPublishIntegrationFixture(t)

	store := NewOutboxStore(
		fixture.pool,
	)

	tests := []struct {
		name  string
		input outboxapp.MarkPublishedInput
	}{
		{
			name: "blank event ID",
			input: outboxapp.MarkPublishedInput{
				EventID:     "   ",
				ClaimToken:  "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				PublishedAt: time.Now(),
			},
		},
		{
			name: "blank claim token",
			input: outboxapp.MarkPublishedInput{
				EventID:     "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken:  "   ",
				PublishedAt: time.Now(),
			},
		},
		{
			name: "zero publication time",
			input: outboxapp.MarkPublishedInput{
				EventID:    "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				updated, err := store.MarkPublished(
					fixture.ctx,
					testCase.input,
				)

				if err == nil {
					t.Fatal(
						"MarkPublished() accepted invalid input",
					)
				}

				if updated {
					t.Fatal(
						"MarkPublished() updated event for invalid input",
					)
				}
			},
		)
	}
}
