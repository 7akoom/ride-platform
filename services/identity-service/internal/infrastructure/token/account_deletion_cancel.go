package token

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

// cancelPendingAccountDeletionInTransaction cancels the identity's pending
// account deletion, if any, and writes identity.deletion_cancelled.
func cancelPendingAccountDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	identityID string,
) error {
	now := time.Now().UTC()

	var cancelled string

	err := tx.QueryRow(
		ctx,
		`UPDATE account_deletions
		 SET status = 'cancelled', cancelled_at = $2, next_attempt_at = NULL, updated_at = $2
		 WHERE identity_id = $1::uuid AND status = 'pending'
		 RETURNING identity_id::text`,
		identityID,
		now,
	).Scan(&cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("cancel pending account deletion: %w", err)
	}

	payload, err := json.Marshal(map[string]string{"identity_id": identityID})
	if err != nil {
		return fmt.Errorf("marshal deletion cancelled payload: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		     (aggregate_type, aggregate_id, event_type, schema_version, payload, occurred_at, available_at)
		 VALUES ('identity', $1::uuid, $2, $3, $4::jsonb, $5, $5)`,
		identityID,
		string(auth.IdentityDomainEventDeletionCancelled),
		auth.IdentityDomainEventSchemaVersion,
		payload,
		now,
	); err != nil {
		return fmt.Errorf("insert deletion cancelled outbox event: %w", err)
	}

	return nil
}
