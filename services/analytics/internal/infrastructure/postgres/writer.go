package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Writer is the ingest side: every method runs inside one event's
// transaction, so marking the event processed and changing the facts land
// together or not at all.
type Writer struct {
	pool *pgxpool.Pool
}

func NewWriter(pool *pgxpool.Pool) *Writer {
	return &Writer{pool: pool}
}

// WithTx runs fn in one transaction, committing on success.
func (w *Writer) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// MarkProcessed records the event; false means it was already counted.
func (w *Writer) MarkProcessed(ctx context.Context, tx pgx.Tx, eventID, eventType string, occurredAt time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, event_type, occurred_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING`,
		eventID, eventType, occurredAt)
	if err != nil {
		return false, fmt.Errorf("record processed event: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// TripRequested is what trip.requested says about a trip.
type TripRequested struct {
	TripID        string
	RiderID       string
	CityID        string
	ZoneID        string
	VehicleClass  string
	PaymentMethod string
	Scheduled     *bool
	At            time.Time
}

func (w *Writer) TripRequested(ctx context.Context, tx pgx.Tx, t TripRequested) error {
	return exec(ctx, tx, "record trip request", `
		INSERT INTO trip_facts (trip_id, rider_id, city_id, zone_id, vehicle_class, payment_method, scheduled, requested_at)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8)
		ON CONFLICT (trip_id) DO UPDATE SET
			rider_id       = COALESCE(trip_facts.rider_id, EXCLUDED.rider_id),
			city_id        = COALESCE(trip_facts.city_id, EXCLUDED.city_id),
			zone_id        = COALESCE(trip_facts.zone_id, EXCLUDED.zone_id),
			vehicle_class  = COALESCE(trip_facts.vehicle_class, EXCLUDED.vehicle_class),
			payment_method = COALESCE(trip_facts.payment_method, EXCLUDED.payment_method),
			scheduled      = COALESCE(trip_facts.scheduled, EXCLUDED.scheduled),
			requested_at   = COALESCE(trip_facts.requested_at, EXCLUDED.requested_at)`,
		t.TripID, t.RiderID, t.CityID, t.ZoneID, t.VehicleClass, t.PaymentMethod, t.Scheduled, t.At)
}

func (w *Writer) TripAccepted(ctx context.Context, tx pgx.Tx, tripID, driverID string, at time.Time) error {
	return exec(ctx, tx, "record trip acceptance", `
		INSERT INTO trip_facts (trip_id, driver_id, accepted_at)
		VALUES ($1, NULLIF($2, ''), $3)
		ON CONFLICT (trip_id) DO UPDATE SET
			driver_id   = COALESCE(trip_facts.driver_id, EXCLUDED.driver_id),
			accepted_at = COALESCE(trip_facts.accepted_at, EXCLUDED.accepted_at)`,
		tripID, driverID, at)
}

func (w *Writer) TripArrived(ctx context.Context, tx pgx.Tx, tripID string, at time.Time) error {
	return w.setOnce(ctx, tx, "arrived_at", tripID, at)
}

func (w *Writer) TripStarted(ctx context.Context, tx pgx.Tx, tripID string, at time.Time) error {
	return w.setOnce(ctx, tx, "started_at", tripID, at)
}

func (w *Writer) TripCompleted(ctx context.Context, tx pgx.Tx, tripID, riderID, driverID string, at time.Time) error {
	return exec(ctx, tx, "record trip completion", `
		INSERT INTO trip_facts (trip_id, rider_id, driver_id, completed_at)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4)
		ON CONFLICT (trip_id) DO UPDATE SET
			rider_id     = COALESCE(trip_facts.rider_id, EXCLUDED.rider_id),
			driver_id    = COALESCE(trip_facts.driver_id, EXCLUDED.driver_id),
			completed_at = COALESCE(trip_facts.completed_at, EXCLUDED.completed_at)`,
		tripID, riderID, driverID, at)
}

// TripCancelled is what trip.cancelled says. Stage is empty for events
// written before trip-service named it; the stage then follows from what
// the trip had reached.
type TripCancelled struct {
	TripID      string
	CancelledBy string
	RiderNoShow bool
	Stage       string
	At          time.Time
}

func (w *Writer) TripCancelled(ctx context.Context, tx pgx.Tx, c TripCancelled) error {
	return exec(ctx, tx, "record trip cancellation", `
		INSERT INTO trip_facts (trip_id, cancelled_at, cancelled_by, rider_no_show, cancel_stage)
		VALUES ($1, $2, NULLIF($3, ''), $4, COALESCE(NULLIF($5, ''), 'requested'))
		ON CONFLICT (trip_id) DO UPDATE SET
			cancelled_at  = COALESCE(trip_facts.cancelled_at, EXCLUDED.cancelled_at),
			cancelled_by  = COALESCE(trip_facts.cancelled_by, EXCLUDED.cancelled_by),
			rider_no_show = trip_facts.rider_no_show OR EXCLUDED.rider_no_show,
			cancel_stage  = COALESCE(trip_facts.cancel_stage, NULLIF($5, ''), CASE
				WHEN trip_facts.started_at IS NOT NULL THEN 'started'
				WHEN trip_facts.arrived_at IS NOT NULL THEN 'arrived'
				WHEN trip_facts.accepted_at IS NOT NULL THEN 'accepted'
				ELSE 'requested'
			END)`,
		c.TripID, c.At, c.CancelledBy, c.RiderNoShow, c.Stage)
}

// Fare is what fare.calculated says. A later calculation replaces an
// earlier one.
type Fare struct {
	TripID   string
	RiderID  string
	Kind     string
	Currency string
	Total    decimal.Decimal
	At       time.Time
}

func (w *Writer) FareCalculated(ctx context.Context, tx pgx.Tx, f Fare) error {
	return exec(ctx, tx, "record fare", `
		INSERT INTO trip_facts (trip_id, rider_id, fare_kind, currency, fare_total, fare_at)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6)
		ON CONFLICT (trip_id) DO UPDATE SET
			rider_id   = COALESCE(trip_facts.rider_id, EXCLUDED.rider_id),
			fare_kind  = CASE WHEN trip_facts.fare_at > EXCLUDED.fare_at THEN trip_facts.fare_kind ELSE EXCLUDED.fare_kind END,
			currency   = CASE WHEN trip_facts.fare_at > EXCLUDED.fare_at THEN trip_facts.currency ELSE EXCLUDED.currency END,
			fare_total = CASE WHEN trip_facts.fare_at > EXCLUDED.fare_at THEN trip_facts.fare_total ELSE EXCLUDED.fare_total END,
			fare_at    = GREATEST(trip_facts.fare_at, EXCLUDED.fare_at)`,
		f.TripID, f.RiderID, f.Kind, f.Currency, f.Total, f.At)
}

func (w *Writer) TripSettled(ctx context.Context, tx pgx.Tx, tripID, driverID string, commission decimal.Decimal, at time.Time) error {
	return exec(ctx, tx, "record settlement", `
		INSERT INTO trip_facts (trip_id, driver_id, commission, settled_at)
		VALUES ($1, NULLIF($2, ''), $3, $4)
		ON CONFLICT (trip_id) DO UPDATE SET
			driver_id  = COALESCE(trip_facts.driver_id, EXCLUDED.driver_id),
			commission = CASE WHEN trip_facts.settled_at > EXCLUDED.settled_at THEN trip_facts.commission ELSE EXCLUDED.commission END,
			settled_at = GREATEST(trip_facts.settled_at, EXCLUDED.settled_at)`,
		tripID, driverID, commission, at)
}

func (w *Writer) RiderSignedUp(ctx context.Context, tx pgx.Tx, riderID string, at time.Time) error {
	return exec(ctx, tx, "record rider signup", `
		INSERT INTO rider_signups (rider_id, signed_up_at) VALUES ($1, $2)
		ON CONFLICT (rider_id) DO UPDATE SET signed_up_at = LEAST(rider_signups.signed_up_at, EXCLUDED.signed_up_at)`,
		riderID, at)
}

func (w *Writer) DriverSignedUp(ctx context.Context, tx pgx.Tx, driverID string, at time.Time) error {
	return exec(ctx, tx, "record driver signup", `
		INSERT INTO driver_signups (driver_id, signed_up_at) VALUES ($1, $2)
		ON CONFLICT (driver_id) DO UPDATE SET signed_up_at = COALESCE(LEAST(driver_signups.signed_up_at, EXCLUDED.signed_up_at), EXCLUDED.signed_up_at)`,
		driverID, at)
}

// DriverApproved keeps the first approval: a driver's cohort is the week
// they could start driving.
func (w *Writer) DriverApproved(ctx context.Context, tx pgx.Tx, driverID string, at time.Time) error {
	return exec(ctx, tx, "record driver approval", `
		INSERT INTO driver_signups (driver_id, approved_at) VALUES ($1, $2)
		ON CONFLICT (driver_id) DO UPDATE SET approved_at = COALESCE(LEAST(driver_signups.approved_at, EXCLUDED.approved_at), EXCLUDED.approved_at)`,
		driverID, at)
}

// setOnce fills a timestamp column the first time; column is a literal
// from this file, never caller input.
func (w *Writer) setOnce(ctx context.Context, tx pgx.Tx, column, tripID string, at time.Time) error {
	return exec(ctx, tx, "record "+column, fmt.Sprintf(`
		INSERT INTO trip_facts (trip_id, %[1]s) VALUES ($1, $2)
		ON CONFLICT (trip_id) DO UPDATE SET %[1]s = COALESCE(trip_facts.%[1]s, EXCLUDED.%[1]s)`, column),
		tripID, at)
}

func exec(ctx context.Context, tx pgx.Tx, what, sql string, args ...any) error {
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	return nil
}
