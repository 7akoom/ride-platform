package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

// Writer is the ingest-side repository: every method here is called from
// inside a single event handler's transaction, so a consumer can insert the
// raw event and update its one derived table atomically — either both land
// or neither does, keeping raw_events and the aggregates it feeds always in
// sync even across crashes/redeliveries.
type Writer struct {
	pool *pgxpool.Pool
}

func NewWriter(pool *pgxpool.Pool) *Writer {
	return &Writer{pool: pool}
}

// WithTx runs fn inside a single transaction, committing on success and
// rolling back on any error (including a panic recovered further up the
// call stack — Rollback on an already-committed tx is a documented no-op).
func (w *Writer) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// InsertRawEvent stores the event verbatim, keyed by its own event_id for
// idempotency. Returns false (no error) when the event_id was already
// present — the caller uses this to skip updating derived tables on a
// JetStream redelivery, so counts never get double-counted.
func (w *Writer) InsertRawEvent(
	ctx context.Context,
	tx pgx.Tx,
	eventID, eventType string,
	payload []byte,
	occurredAt time.Time,
) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO raw_events (event_id, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID, eventType, payload, occurredAt)
	if err != nil {
		return false, fmt.Errorf("insert raw event: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// --- Trip funnel ---

func (w *Writer) IncrementFunnelRequested(ctx context.Context, tx pgx.Tx, day time.Time) error {
	return w.incrementFunnelColumn(ctx, tx, day, "requested_count")
}

func (w *Writer) IncrementFunnelAccepted(ctx context.Context, tx pgx.Tx, day time.Time) error {
	return w.incrementFunnelColumn(ctx, tx, day, "accepted_count")
}

func (w *Writer) IncrementFunnelStarted(ctx context.Context, tx pgx.Tx, day time.Time) error {
	return w.incrementFunnelColumn(ctx, tx, day, "started_count")
}

func (w *Writer) IncrementFunnelCompleted(ctx context.Context, tx pgx.Tx, day time.Time) error {
	return w.incrementFunnelColumn(ctx, tx, day, "completed_count")
}

func (w *Writer) IncrementFunnelCancelled(ctx context.Context, tx pgx.Tx, day time.Time) error {
	return w.incrementFunnelColumn(ctx, tx, day, "cancelled_count")
}

// incrementFunnelColumn is unexported and only ever called with one of the
// five hardcoded literal column names above — never with caller-controlled
// input — so building the column name into the query string here is safe.
func (w *Writer) incrementFunnelColumn(ctx context.Context, tx pgx.Tx, day time.Time, column string) error {
	query := fmt.Sprintf(`
		INSERT INTO trip_funnel_daily (day, %[1]s)
		VALUES ($1, 1)
		ON CONFLICT (day) DO UPDATE SET %[1]s = trip_funnel_daily.%[1]s + 1
	`, column)

	if _, err := tx.Exec(ctx, query, day); err != nil {
		return fmt.Errorf("increment funnel %s: %w", column, err)
	}

	return nil
}

// --- Cancellations & trip stage tracking ---

// SetTripStage records the latest known stage for a trip. Called on every
// trip.* event except trip.cancelled itself, so that when trip.cancelled
// arrives we can look up what stage it was cancelled from (the cancellation
// event carries no memory of the trip's prior state on its own).
func (w *Writer) SetTripStage(ctx context.Context, tx pgx.Tx, tripID string, stage domain.TripStage, at time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO trip_last_known_stage (trip_id, stage, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (trip_id) DO UPDATE SET stage = $2, updated_at = $3
	`, tripID, string(stage), at)
	if err != nil {
		return fmt.Errorf("set trip stage: %w", err)
	}

	return nil
}

// GetTripStage returns the last recorded stage for a trip, or
// domain.TripStageRequested with found=false if none was ever recorded
// (e.g. trip.requested's own outbox event hasn't been delivered yet).
func (w *Writer) GetTripStage(ctx context.Context, tx pgx.Tx, tripID string) (stage domain.TripStage, found bool, err error) {
	var raw string

	err = tx.QueryRow(ctx, `
		SELECT stage FROM trip_last_known_stage WHERE trip_id = $1
	`, tripID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TripStageRequested, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get trip stage: %w", err)
	}

	return domain.TripStage(raw), true, nil
}

// RecordCancellation stores which stage a trip was cancelled from. Idempotent
// on trip_id — a trip can only be cancelled once.
func (w *Writer) RecordCancellation(ctx context.Context, tx pgx.Tx, tripID string, stage domain.TripStage, cancelledAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO trip_cancellations (trip_id, stage_at_cancellation, cancelled_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (trip_id) DO NOTHING
	`, tripID, string(stage), cancelledAt)
	if err != nil {
		return fmt.Errorf("record cancellation: %w", err)
	}

	return nil
}

// --- Revenue ---

// RecordRevenue adds gross/commission deltas and a trip-count delta into the
// (day, currency) bucket, creating it if absent.
func (w *Writer) RecordRevenue(
	ctx context.Context,
	tx pgx.Tx,
	day time.Time,
	currency string,
	grossDelta, commissionDelta decimal.Decimal,
	tripCountDelta int64,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO revenue_daily (day, currency, gross_fare_total, commission_total, trip_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (day, currency) DO UPDATE SET
			gross_fare_total = revenue_daily.gross_fare_total + $3,
			commission_total = revenue_daily.commission_total + $4,
			trip_count = revenue_daily.trip_count + $5
	`, day, currency, grossDelta, commissionDelta, tripCountDelta)
	if err != nil {
		return fmt.Errorf("record revenue: %w", err)
	}

	return nil
}

// --- Cohorts & weekly activity (weeks stored as their Monday DATE) ---

// RecordRiderCohort assigns a rider to their signup cohort week the first
// time it's called for that rider; subsequent calls (there shouldn't be any,
// since rider.created fires once, but redeliveries are possible) no-op.
func (w *Writer) RecordRiderCohort(ctx context.Context, tx pgx.Tx, riderID string, cohortWeekStart time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO rider_cohorts (rider_id, cohort_week_start)
		VALUES ($1, $2)
		ON CONFLICT (rider_id) DO NOTHING
	`, riderID, cohortWeekStart)
	if err != nil {
		return fmt.Errorf("record rider cohort: %w", err)
	}

	return nil
}

func (w *Writer) RecordDriverCohort(ctx context.Context, tx pgx.Tx, driverID string, cohortWeekStart time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO driver_cohorts (driver_id, cohort_week_start)
		VALUES ($1, $2)
		ON CONFLICT (driver_id) DO NOTHING
	`, driverID, cohortWeekStart)
	if err != nil {
		return fmt.Errorf("record driver cohort: %w", err)
	}

	return nil
}

// RecordRiderActivity marks a rider as active in the given week (called on
// trip.requested) — used to compute retention against their cohort week.
func (w *Writer) RecordRiderActivity(ctx context.Context, tx pgx.Tx, riderID string, activityWeekStart time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO rider_weekly_activity (rider_id, activity_week_start)
		VALUES ($1, $2)
		ON CONFLICT (rider_id, activity_week_start) DO NOTHING
	`, riderID, activityWeekStart)
	if err != nil {
		return fmt.Errorf("record rider activity: %w", err)
	}

	return nil
}

// RecordDriverActivity marks a driver as active in the given week (called on
// trip.accepted) — used to compute retention against their cohort week.
func (w *Writer) RecordDriverActivity(ctx context.Context, tx pgx.Tx, driverID string, activityWeekStart time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO driver_weekly_activity (driver_id, activity_week_start)
		VALUES ($1, $2)
		ON CONFLICT (driver_id, activity_week_start) DO NOTHING
	`, driverID, activityWeekStart)
	if err != nil {
		return fmt.Errorf("record driver activity: %w", err)
	}

	return nil
}

// --- Trip currency lookup (needed because trip.settled carries no
// currency_code of its own — only fare.calculated does) ---

func (w *Writer) RecordTripCurrency(ctx context.Context, tx pgx.Tx, tripID, currency string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO trip_fare_currency (trip_id, currency)
		VALUES ($1, $2)
		ON CONFLICT (trip_id) DO NOTHING
	`, tripID, currency)
	if err != nil {
		return fmt.Errorf("record trip currency: %w", err)
	}

	return nil
}

// GetTripCurrency returns the currency recorded for a trip, or ("", false)
// if fare.calculated hasn't been processed for it yet (e.g. arrived out of
// order — the caller should fall back to a default rather than fail).
func (w *Writer) GetTripCurrency(ctx context.Context, tx pgx.Tx, tripID string) (currency string, found bool, err error) {
	err = tx.QueryRow(ctx, `
		SELECT currency FROM trip_fare_currency WHERE trip_id = $1
	`, tripID).Scan(&currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get trip currency: %w", err)
	}

	return currency, true, nil
}
