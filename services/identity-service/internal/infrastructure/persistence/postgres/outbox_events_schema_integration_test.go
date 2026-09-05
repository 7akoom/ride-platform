//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outboxSchemaIntegrationFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
}

func newOutboxSchemaIntegrationFixture(
	t *testing.T,
) *outboxSchemaIntegrationFixture {
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

	return &outboxSchemaIntegrationFixture{
		t:    t,
		ctx:  ctx,
		pool: pool,
	}
}

func TestOutboxEventsSchemaAcceptsValidEvent(
	t *testing.T,
) {
	fixture :=
		newOutboxSchemaIntegrationFixture(t)

	const aggregateID = "11111111-1111-1111-1111-111111111111"

	occurredAt := time.Now().UTC()

	var eventID string

	err := fixture.pool.QueryRow(
		fixture.ctx,
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
				$6
			)
			RETURNING id::text
		`,
		"identity",
		aggregateID,
		"identity.suspended",
		int16(1),
		`{
			"identity_id":
				"11111111-1111-1111-1111-111111111111",
			"previous_status": "active",
			"current_status": "suspended"
		}`,
		occurredAt,
	).Scan(
		&eventID,
	)
	if err != nil {
		t.Fatalf(
			"insert valid outbox event: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := fixture.pool.Exec(
			context.Background(),
			`
				DELETE FROM outbox_events
				WHERE id = $1::uuid
			`,
			eventID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean outbox event: %v",
				cleanupErr,
			)
		}
	})

	if eventID == "" {
		t.Fatal(
			"outbox event ID is empty",
		)
	}
}

func TestOutboxEventsSchemaRejectsInvalidRows(
	t *testing.T,
) {
	fixture :=
		newOutboxSchemaIntegrationFixture(t)

	const aggregateID = "22222222-2222-2222-2222-222222222222"

	now := time.Now().UTC()

	tests := []struct {
		name          string
		aggregateType string
		eventType     string
		schemaVersion int16
		payload       string
		attempts      int
	}{
		{
			name:          "blank aggregate type",
			aggregateType: "   ",
			eventType:     "identity.suspended",
			schemaVersion: 1,
			payload:       `{}`,
			attempts:      0,
		},
		{
			name:          "untrimmed aggregate type",
			aggregateType: " identity ",
			eventType:     "identity.suspended",
			schemaVersion: 1,
			payload:       `{}`,
			attempts:      0,
		},
		{
			name:          "blank event type",
			aggregateType: "identity",
			eventType:     "   ",
			schemaVersion: 1,
			payload:       `{}`,
			attempts:      0,
		},
		{
			name:          "untrimmed event type",
			aggregateType: "identity",
			eventType:     " identity.suspended ",
			schemaVersion: 1,
			payload:       `{}`,
			attempts:      0,
		},
		{
			name:          "zero schema version",
			aggregateType: "identity",
			eventType:     "identity.suspended",
			schemaVersion: 0,
			payload:       `{}`,
			attempts:      0,
		},
		{
			name:          "payload is not object",
			aggregateType: "identity",
			eventType:     "identity.suspended",
			schemaVersion: 1,
			payload:       `[]`,
			attempts:      0,
		},
		{
			name:          "negative publish attempts",
			aggregateType: "identity",
			eventType:     "identity.suspended",
			schemaVersion: 1,
			payload:       `{}`,
			attempts:      -1,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				_, err := fixture.pool.Exec(
					fixture.ctx,
					`
						INSERT INTO outbox_events (
							aggregate_type,
							aggregate_id,
							event_type,
							schema_version,
							payload,
							occurred_at,
							available_at,
							publish_attempts
						)
						VALUES (
							$1,
							$2::uuid,
							$3,
							$4,
							$5::jsonb,
							$6,
							$6,
							$7
						)
					`,
					tt.aggregateType,
					aggregateID,
					tt.eventType,
					tt.schemaVersion,
					tt.payload,
					now,
					tt.attempts,
				)

				if err == nil {
					t.Fatal(
						"invalid outbox row was accepted",
					)
				}
			},
		)
	}
}
