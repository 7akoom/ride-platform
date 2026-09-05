package postgres

import (
	"context"
	"errors"
	"fmt"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxOutboxClaimBatchSize = 1000

type OutboxStore struct {
	pool *pgxpool.Pool
}

func NewOutboxStore(
	pool *pgxpool.Pool,
) *OutboxStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &OutboxStore{
		pool: pool,
	}
}

func (s *OutboxStore) ClaimPending(
	ctx context.Context,
	input outboxapp.ClaimPendingInput,
) ([]outboxapp.Event, error) {
	if input.ClaimedAt.IsZero() {
		return nil, errors.New(
			"outbox claim time cannot be zero",
		)
	}

	if input.LeaseDuration <= 0 {
		return nil, errors.New(
			"outbox lease duration must be positive",
		)
	}

	if input.Limit <= 0 {
		return nil, errors.New(
			"outbox claim limit must be positive",
		)
	}

	if input.Limit > maxOutboxClaimBatchSize {
		return nil, fmt.Errorf(
			"outbox claim limit cannot exceed %d",
			maxOutboxClaimBatchSize,
		)
	}

	claimedAt := input.ClaimedAt.UTC()
	leaseUntil := claimedAt.Add(
		input.LeaseDuration,
	)

	if !leaseUntil.After(claimedAt) {
		return nil, errors.New(
			"outbox lease expiration must be after claim time",
		)
	}

	const claimPendingQuery = `
		WITH candidates AS (
			SELECT
				id,
				available_at AS claim_order_available_at,
				occurred_at AS claim_order_occurred_at
			FROM outbox_events
			WHERE published_at IS NULL
			  AND available_at <= $1
			ORDER BY
				available_at ASC,
				occurred_at ASC,
				id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		),
		claimed AS (
			UPDATE outbox_events AS event
			SET
				claim_token = gen_random_uuid(),
				claimed_at = $1,
				available_at = $3,
				publish_attempts =
					event.publish_attempts + 1,
				last_error = NULL,
				updated_at = $1
			FROM candidates
			WHERE event.id = candidates.id
			RETURNING
				event.id,
				event.aggregate_type,
				event.aggregate_id,
				event.event_type,
				event.schema_version,
				event.payload,
				event.occurred_at,
				event.claim_token,
				event.publish_attempts
		)
		SELECT
			claimed.id::text,
			claimed.aggregate_type,
			claimed.aggregate_id::text,
			claimed.event_type,
			claimed.schema_version,
			claimed.payload,
			claimed.occurred_at,
			claimed.claim_token::text,
			claimed.publish_attempts
		FROM claimed
		INNER JOIN candidates
			ON candidates.id = claimed.id
		ORDER BY
			candidates.claim_order_available_at ASC,
			candidates.claim_order_occurred_at ASC,
			candidates.id ASC
	`

	rows, err := s.pool.Query(
		ctx,
		claimPendingQuery,
		claimedAt,
		input.Limit,
		leaseUntil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"claim pending outbox events: %w",
			err,
		)
	}
	defer rows.Close()

	events := make(
		[]outboxapp.Event,
		0,
		input.Limit,
	)

	for rows.Next() {
		var event outboxapp.Event
		var payload []byte

		if err := rows.Scan(
			&event.ID,
			&event.AggregateType,
			&event.AggregateID,
			&event.EventType,
			&event.SchemaVersion,
			&payload,
			&event.OccurredAt,
			&event.ClaimToken,
			&event.PublishAttempts,
		); err != nil {
			return nil, fmt.Errorf(
				"scan claimed outbox event: %w",
				err,
			)
		}

		event.Payload = append(
			event.Payload[:0],
			payload...,
		)

		event.OccurredAt =
			event.OccurredAt.UTC()

		events = append(
			events,
			event,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate claimed outbox events: %w",
			err,
		)
	}

	return events, nil
}
