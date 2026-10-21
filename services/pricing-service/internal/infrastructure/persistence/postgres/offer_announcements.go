package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// The events that tell riders of an offer (notification-service pushes them).
const (
	eventLoyaltyDue    = "fare.offer.loyalty_due"
	eventFeaturedOffer = "fare.offer.featured"
)

// countCompletedTrip counts the rider's completed trip, and when the next one is
// the loyalty discount's, writes fare.offer.loyalty_due in the same transaction.
func countCompletedTrip(ctx context.Context, tx pgx.Tx, riderID string) error {
	var completed int

	if err := tx.QueryRow(ctx,
		`INSERT INTO rider_trip_stats (rider_id, completed_trip_count, first_completed_at)
		 VALUES ($1, 1, CURRENT_TIMESTAMP)
		 ON CONFLICT (rider_id) DO UPDATE
		 SET completed_trip_count = rider_trip_stats.completed_trip_count + 1,
		     updated_at = CURRENT_TIMESTAMP
		 RETURNING completed_trip_count`,
		riderID,
	).Scan(&completed); err != nil {
		return fmt.Errorf("update rider trip stats: %w", err)
	}

	var (
		every   int
		percent string
		max     *string
	)

	err := tx.QueryRow(ctx,
		`SELECT loyalty_every, loyalty_percent::text, loyalty_max_amount::text FROM promotion_settings WHERE id`,
	).Scan(&every, &percent, &max)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read the loyalty settings: %w", err)
	case every <= 0 || (completed+1)%every != 0 || plainDecimal(percent) == "0":
		return nil
	}

	payload := map[string]any{
		"rider_id":    riderID,
		"percent":     plainDecimal(percent),
		"trip_number": completed + 1,
	}

	if max != nil {
		payload["max_amount"] = plainDecimal(*max)
	}

	return writeOfferEvent(ctx, tx, eventLoyaltyDue, riderID, payload)
}

// AnnounceFeaturedCoupons marks the featured coupons that are running now and
// were never announced, and writes a fare.offer.featured for each, together. A
// coupon for new riders only is not announced (most riders cannot use it); it
// still shows on the app's home for those who can.
func (r *PricingRepository) AnnounceFeaturedCoupons(ctx context.Context, now time.Time) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`UPDATE coupons
		 SET announced_at = $1
		 WHERE featured AND active AND announced_at IS NULL AND NOT new_riders_only
		   AND valid_from <= $1 AND (valid_until IS NULL OR valid_until > $1)
		   AND (COALESCE(max_redemptions, 0) = 0 OR redemption_count < max_redemptions)
		 RETURNING id::text, code, description, discount_type, discount_value::text,
		           COALESCE(max_discount_amount::text, ''), valid_until`,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("mark featured coupons announced: %w", err)
	}

	type announced struct {
		id      string
		payload map[string]any
	}

	var all []announced

	for rows.Next() {
		var (
			id, code, description, kind, value, max string
			until                                   *time.Time
		)

		if err := rows.Scan(&id, &code, &description, &kind, &value, &max, &until); err != nil {
			rows.Close()

			return 0, fmt.Errorf("read featured coupon: %w", err)
		}

		payload := map[string]any{
			"coupon_code": code, "description": description,
			"discount_type": kind, "discount_value": plainDecimal(value),
		}

		if max != "" {
			payload["max_discount_amount"] = plainDecimal(max)
		}

		if until != nil {
			payload["valid_until"] = until.UTC().Format(time.RFC3339)
		}

		all = append(all, announced{id: id, payload: payload})
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read featured coupons: %w", err)
	}

	for _, a := range all {
		if err := writeOfferEvent(ctx, tx, eventFeaturedOffer, a.id, a.payload); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return len(all), nil
}

// plainDecimal shows a NUMERIC without the zeros of its scale ("20.00" as "20").
func plainDecimal(value string) string {
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		return value
	}

	return parsed.String()
}

func writeOfferEvent(ctx context.Context, tx pgx.Tx, eventType, aggregateID string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, schema_version, payload, occurred_at, available_at)
		 VALUES ('offer', $1, $2, $3, $4, $5, $5)`,
		aggregateID, eventType, schemaVersion, body, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}

	return nil
}
