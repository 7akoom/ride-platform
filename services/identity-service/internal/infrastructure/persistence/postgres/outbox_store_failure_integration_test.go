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

type outboxFailureIntegrationFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
}

func newOutboxFailureIntegrationFixture(
	t *testing.T,
) *outboxFailureIntegrationFixture {
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

	return &outboxFailureIntegrationFixture{
		t:    t,
		ctx:  ctx,
		pool: pool,
	}
}

func (f *outboxFailureIntegrationFixture) createClaimedEvent(
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
			"create failure test outbox event: %v",
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
				"clean failure test outbox event: %v",
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
			"claim failure test event: %v",
			err,
		)
	}

	if len(events) != 1 {
		f.t.Fatalf(
			"claimed failure test events = %d, expected 1",
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

func (f *outboxFailureIntegrationFixture) failureState(
	eventID string,
) (
	bool,
	bool,
	time.Time,
	string,
	int,
) {
	f.t.Helper()

	var hasClaimToken bool
	var hasClaimedAt bool
	var availableAt time.Time
	var lastError *string
	var publishAttempts int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT
				claim_token IS NOT NULL,
				claimed_at IS NOT NULL,
				available_at,
				last_error,
				publish_attempts
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		eventID,
	).Scan(
		&hasClaimToken,
		&hasClaimedAt,
		&availableAt,
		&lastError,
		&publishAttempts,
	)
	if err != nil {
		f.t.Fatalf(
			"query outbox failure state: %v",
			err,
		)
	}

	lastErrorValue := ""
	if lastError != nil {
		lastErrorValue = *lastError
	}

	return hasClaimToken,
		hasClaimedAt,
		availableAt.UTC(),
		lastErrorValue,
		publishAttempts
}

func TestOutboxStoreMarkFailedSchedulesRetry(
	t *testing.T,
) {
	fixture :=
		newOutboxFailureIntegrationFixture(t)

	claimedAt := time.Date(
		2002,
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

	failedAt :=
		claimedAt.Add(10 * time.Second)

	retryAt :=
		failedAt.Add(30 * time.Second)

	store := NewOutboxStore(
		fixture.pool,
	)

	updated, err := store.MarkFailed(
		fixture.ctx,
		outboxapp.MarkFailedInput{
			EventID:      eventID,
			ClaimToken:   claimToken,
			FailedAt:     failedAt,
			RetryAt:      retryAt,
			ErrorMessage: "broker unavailable",
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkFailed() returned an error: %v",
			err,
		)
	}

	if !updated {
		t.Fatal(
			"MarkFailed() did not update claimed event",
		)
	}

	hasClaimToken,
		hasClaimedAt,
		availableAt,
		lastError,
		publishAttempts :=
		fixture.failureState(eventID)

	if hasClaimToken {
		t.Fatal(
			"failed event retained claim token",
		)
	}

	if hasClaimedAt {
		t.Fatal(
			"failed event retained claimed_at",
		)
	}

	if !availableAt.Equal(retryAt.UTC()) {
		t.Fatalf(
			"available at = %v, expected %v",
			availableAt,
			retryAt.UTC(),
		)
	}

	if lastError != "broker unavailable" {
		t.Fatalf(
			"last error = %q, expected %q",
			lastError,
			"broker unavailable",
		)
	}

	if publishAttempts != 1 {
		t.Fatalf(
			"publish attempts = %d, expected 1",
			publishAttempts,
		)
	}
}

func TestOutboxStoreMarkFailedRejectsStaleClaimToken(
	t *testing.T,
) {
	fixture :=
		newOutboxFailureIntegrationFixture(t)

	claimedAt := time.Date(
		2002,
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

	updated, err := store.MarkFailed(
		fixture.ctx,
		outboxapp.MarkFailedInput{
			EventID:    eventID,
			ClaimToken: firstClaimToken,
			FailedAt: claimedAt.Add(
				40 * time.Second,
			),
			RetryAt: claimedAt.Add(
				time.Minute,
			),
			ErrorMessage: "stale worker failure",
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkFailed() returned an error: %v",
			err,
		)
	}

	if updated {
		t.Fatal(
			"stale claim token updated event",
		)
	}

	hasClaimToken,
		hasClaimedAt,
		_,
		lastError,
		publishAttempts :=
		fixture.failureState(eventID)

	if !hasClaimToken {
		t.Fatal(
			"stale failure cleared active claim token",
		)
	}

	if !hasClaimedAt {
		t.Fatal(
			"stale failure cleared active claimed_at",
		)
	}

	if lastError != "" {
		t.Fatalf(
			"stale failure changed last error to %q",
			lastError,
		)
	}

	if publishAttempts != 2 {
		t.Fatalf(
			"publish attempts = %d, expected 2",
			publishAttempts,
		)
	}
}

func TestOutboxStoreMarkFailedBecomesClaimableAtRetryTime(
	t *testing.T,
) {
	fixture :=
		newOutboxFailureIntegrationFixture(t)

	claimedAt := time.Date(
		2002,
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

	failedAt :=
		claimedAt.Add(10 * time.Second)

	retryAt :=
		failedAt.Add(time.Minute)

	store := NewOutboxStore(
		fixture.pool,
	)

	updated, err := store.MarkFailed(
		fixture.ctx,
		outboxapp.MarkFailedInput{
			EventID:      eventID,
			ClaimToken:   claimToken,
			FailedAt:     failedAt,
			RetryAt:      retryAt,
			ErrorMessage: "temporary publish failure",
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkFailed() returned an error: %v",
			err,
		)
	}

	if !updated {
		t.Fatal(
			"MarkFailed() did not update event",
		)
	}

	beforeRetry, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     retryAt.Add(-time.Second),
			LeaseDuration: time.Minute,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"ClaimPending() before retry returned an error: %v",
			err,
		)
	}

	if len(beforeRetry) != 0 {
		t.Fatal(
			"failed event became claimable before retry time",
		)
	}

	atRetry, err := store.ClaimPending(
		fixture.ctx,
		outboxapp.ClaimPendingInput{
			ClaimedAt:     retryAt,
			LeaseDuration: time.Minute,
			Limit:         1,
		},
	)
	if err != nil {
		t.Fatalf(
			"ClaimPending() at retry returned an error: %v",
			err,
		)
	}

	if len(atRetry) != 1 {
		t.Fatalf(
			"retry claim returned %d events, expected 1",
			len(atRetry),
		)
	}

	if atRetry[0].ID != eventID {
		t.Fatalf(
			"retry claimed event ID = %q, expected %q",
			atRetry[0].ID,
			eventID,
		)
	}

	if atRetry[0].ClaimToken == claimToken {
		t.Fatal(
			"retry claim reused previous claim token",
		)
	}

	_,
		_,
		_,
		lastError,
		publishAttempts :=
		fixture.failureState(eventID)

	if lastError != "" {
		t.Fatalf(
			"new claim did not clear last error: %q",
			lastError,
		)
	}

	if publishAttempts != 2 {
		t.Fatalf(
			"publish attempts = %d, expected 2",
			publishAttempts,
		)
	}
}

func TestOutboxStoreMarkFailedReturnsFalseForMissingEvent(
	t *testing.T,
) {
	fixture :=
		newOutboxFailureIntegrationFixture(t)

	store := NewOutboxStore(
		fixture.pool,
	)

	now := time.Now()

	updated, err := store.MarkFailed(
		fixture.ctx,
		outboxapp.MarkFailedInput{
			EventID:      "ffffffff-ffff-4fff-8fff-ffffffffffff",
			ClaimToken:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			FailedAt:     now,
			RetryAt:      now.Add(time.Minute),
			ErrorMessage: "publish failed",
		},
	)
	if err != nil {
		t.Fatalf(
			"MarkFailed() returned an error: %v",
			err,
		)
	}

	if updated {
		t.Fatal(
			"missing event was marked failed",
		)
	}
}

func TestOutboxStoreMarkFailedRejectsInvalidInput(
	t *testing.T,
) {
	fixture :=
		newOutboxFailureIntegrationFixture(t)

	store := NewOutboxStore(
		fixture.pool,
	)

	now := time.Now()

	tests := []struct {
		name  string
		input outboxapp.MarkFailedInput
	}{
		{
			name: "blank event ID",
			input: outboxapp.MarkFailedInput{
				EventID:      "   ",
				ClaimToken:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				FailedAt:     now,
				RetryAt:      now.Add(time.Minute),
				ErrorMessage: "publish failed",
			},
		},
		{
			name: "blank claim token",
			input: outboxapp.MarkFailedInput{
				EventID:      "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken:   "   ",
				FailedAt:     now,
				RetryAt:      now.Add(time.Minute),
				ErrorMessage: "publish failed",
			},
		},
		{
			name: "zero failure time",
			input: outboxapp.MarkFailedInput{
				EventID:      "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				RetryAt:      now.Add(time.Minute),
				ErrorMessage: "publish failed",
			},
		},
		{
			name: "zero retry time",
			input: outboxapp.MarkFailedInput{
				EventID:      "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				FailedAt:     now,
				ErrorMessage: "publish failed",
			},
		},
		{
			name: "retry before failure",
			input: outboxapp.MarkFailedInput{
				EventID:      "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				FailedAt:     now,
				RetryAt:      now.Add(-time.Second),
				ErrorMessage: "publish failed",
			},
		},
		{
			name: "blank error message",
			input: outboxapp.MarkFailedInput{
				EventID:      "ffffffff-ffff-4fff-8fff-ffffffffffff",
				ClaimToken:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				FailedAt:     now,
				RetryAt:      now.Add(time.Minute),
				ErrorMessage: "   ",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				updated, err := store.MarkFailed(
					fixture.ctx,
					testCase.input,
				)

				if err == nil {
					t.Fatal(
						"MarkFailed() accepted invalid input",
					)
				}

				if updated {
					t.Fatal(
						"MarkFailed() updated event for invalid input",
					)
				}
			},
		)
	}
}
