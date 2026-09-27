package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// writeOutboxEvent records a driver event in the same transaction as the
// change it describes; the outbox worker publishes it.
func writeOutboxEvent(ctx context.Context, tx pgx.Tx, aggregateID, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	now := time.Now().UTC()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ('driver', $1, $2, 1, $3, $4, $4)`,
		aggregateID,
		eventType,
		body,
		now,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}

	return nil
}
