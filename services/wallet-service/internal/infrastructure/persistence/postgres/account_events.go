package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// Events that tell a person about their own money, for notification-service:
// a driver's wallet crossing the suspension floor either way, a payout paid or
// rejected, a refund, and a top-up through a payment provider that went
// through. Each is written in the same transaction as the money it is about.
const (
	EventDriverSuspended  = "wallet.driver_suspended"
	EventDriverReinstated = "wallet.driver_reinstated"
	EventPayoutPaid       = "wallet.payout_paid"
	EventPayoutRejected   = "wallet.payout_rejected"
	EventRefundIssued     = "wallet.refund_issued"
	EventToppedUp         = "wallet.topped_up"
)

func insertOutboxEvent(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, eventType string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		aggregateType, aggregateID, eventType, schemaVersion, encoded, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}

	return nil
}

// standingChangeEvent tells a driver their wallet crossed the suspension floor:
// suspended (no more trips until they deposit; amount_due clears the balance)
// or reinstated. It is written only when the blocked flag changed.
func standingChangeEvent(ctx context.Context, tx pgx.Tx, before, after wallet.Wallet, floor wallet.Money) error {
	if after.OwnerType != wallet.OwnerDriver || before.Blocked == after.Blocked {
		return nil
	}

	payload := map[string]any{
		"driver_id":        after.OwnerID,
		"balance":          after.Balance.String(),
		"suspension_floor": floor.String(),
		"currency_code":    after.CurrencyCode,
	}

	eventType := EventDriverReinstated

	if after.Blocked {
		eventType = EventDriverSuspended
		payload["amount_due"] = after.Balance.Neg().String()
	}

	return insertOutboxEvent(ctx, tx, "wallet", after.ID, eventType, payload)
}
