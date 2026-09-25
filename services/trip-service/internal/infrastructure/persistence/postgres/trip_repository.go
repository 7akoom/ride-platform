package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const schemaVersion = 1

// activeStatuses are the trip states that count as "in progress" for the
// one-active-trip-per-rider/driver rule.
var activeStatuses = []string{"requested", "accepted", "in_progress"}

type TripRepository struct {
	pool *pgxpool.Pool
}

func NewTripRepository(pool *pgxpool.Pool) *TripRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &TripRepository{pool: pool}
}

func (r *TripRepository) Create(
	ctx context.Context,
	input trip.CreateInput,
) (trip.Trip, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var created trip.Trip

	stops, err := encodeStops(input.Stops)
	if err != nil {
		return trip.Trip{}, err
	}

	row := tx.QueryRow(
		ctx,
		`INSERT INTO trips
		    (id, rider_id, pickup_latitude, pickup_longitude,
		     dropoff_latitude, dropoff_longitude, vehicle_class, payment_method,
		     pickup_address, dropoff_address, pickup_details, pickup_note, pickup_photo_media_id,
		     quote_id, quoted_fare, currency_code, passenger_name, passenger_phone, scheduled, stops)
		 VALUES ($1, $2, $3, $4, $5, $6, COALESCE(NULLIF($7::text, ''), 'economy'),
		         COALESCE(NULLIF($8::text, ''), 'cash'),
		         $9, $10, $11, $12, NULLIF($13::text, '')::uuid,
		         NULLIF($14::text, '')::uuid, NULLIF($15::text, '')::numeric, NULLIF($16::text, ''),
		         $17, $18, $19, $20)
		 RETURNING `+tripColumns,
		input.ID,
		input.RiderID,
		input.Pickup.Latitude,
		input.Pickup.Longitude,
		input.Dropoff.Latitude,
		input.Dropoff.Longitude,
		input.VehicleClass,
		input.PaymentMethod,
		input.PickupAddress,
		input.DropoffAddress,
		input.PickupDetails,
		input.PickupNote,
		input.PickupPhotoMediaID,
		input.QuoteID,
		input.QuotedFare,
		input.CurrencyCode,
		input.PassengerName,
		input.PassengerPhone,
		input.Scheduled,
		stops,
	)

	if err := scanTrip(row, &created); err != nil {
		return trip.Trip{}, fmt.Errorf("insert trip: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, "trip.requested", created.ID, map[string]string{
		"trip_id":  created.ID,
		"rider_id": created.RiderID,
	}); err != nil {
		return trip.Trip{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return trip.Trip{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (r *TripRepository) FindByID(
	ctx context.Context,
	tripID string,
) (trip.Trip, error) {
	return r.findOneWhere(ctx, "id = $1", tripID)
}

func (r *TripRepository) FindActiveByRiderID(
	ctx context.Context,
	riderID string,
) (trip.Trip, error) {
	return r.findOneWhere(ctx, "rider_id = $1 AND status = ANY($2)", riderID, activeStatuses)
}

func (r *TripRepository) FindActiveByDriverID(
	ctx context.Context,
	driverID string,
) (trip.Trip, error) {
	return r.findOneWhere(ctx, "driver_id = $1 AND status = ANY($2)", driverID, activeStatuses)
}

func (r *TripRepository) findOneWhere(
	ctx context.Context,
	whereClause string,
	args ...any,
) (trip.Trip, error) {
	query := `SELECT ` + tripColumns + `
	          FROM trips
	          WHERE ` + whereClause + `
	          ORDER BY created_at DESC
	          LIMIT 1`

	row := r.pool.QueryRow(ctx, query, args...)

	var found trip.Trip

	if err := scanTrip(row, &found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Trip{}, trip.ErrTripNotFound
		}

		return trip.Trip{}, fmt.Errorf("query trip: %w", err)
	}

	return found, nil
}

func (r *TripRepository) Accept(
	ctx context.Context,
	tripID string,
	driverID string,
) (trip.Trip, error) {
	return r.transition(ctx, tripID, trip.StatusAccepted, func(tx pgx.Tx, current trip.Trip) (trip.Trip, error) {
		var updated trip.Trip

		row := tx.QueryRow(
			ctx,
			`UPDATE trips
			 SET driver_id = $2,
			     status = 'accepted',
			     accepted_at = CURRENT_TIMESTAMP,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1
			 RETURNING `+tripColumns,
			tripID,
			driverID,
		)

		if err := scanTrip(row, &updated); err != nil {
			return trip.Trip{}, err
		}

		return updated, writeOutboxEvent(ctx, tx, "trip.accepted", updated.ID, map[string]string{
			"trip_id":   updated.ID,
			"driver_id": driverID,
		})
	})
}

func (r *TripRepository) Start(
	ctx context.Context,
	tripID string,
) (trip.Trip, error) {
	return r.transition(ctx, tripID, trip.StatusInProgress, func(tx pgx.Tx, current trip.Trip) (trip.Trip, error) {
		var updated trip.Trip

		row := tx.QueryRow(
			ctx,
			`UPDATE trips
			 SET status = 'in_progress',
			     started_at = CURRENT_TIMESTAMP,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1
			 RETURNING `+tripColumns,
			tripID,
		)

		if err := scanTrip(row, &updated); err != nil {
			return trip.Trip{}, err
		}

		return updated, writeOutboxEvent(ctx, tx, "trip.started", updated.ID, map[string]string{
			"trip_id": updated.ID,
		})
	})
}

func (r *TripRepository) Complete(
	ctx context.Context,
	tripID string,
) (trip.Trip, error) {
	return r.transition(ctx, tripID, trip.StatusCompleted, func(tx pgx.Tx, current trip.Trip) (trip.Trip, error) {
		var updated trip.Trip

		row := tx.QueryRow(
			ctx,
			`UPDATE trips
			 SET status = 'completed',
			     completed_at = CURRENT_TIMESTAMP,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1
			 RETURNING `+tripColumns,
			tripID,
		)

		if err := scanTrip(row, &updated); err != nil {
			return trip.Trip{}, err
		}

		return updated, writeOutboxEvent(ctx, tx, "trip.completed", updated.ID, map[string]string{
			"trip_id":   updated.ID,
			"rider_id":  updated.RiderID,
			"driver_id": updated.DriverID,
		})
	})
}

func (r *TripRepository) Cancel(
	ctx context.Context,
	record trip.CancelRecord,
) (trip.Trip, error) {
	return r.transition(ctx, record.TripID, trip.StatusCancelled, func(tx pgx.Tx, current trip.Trip) (trip.Trip, error) {
		if record.Allow != nil {
			if err := record.Allow(current); err != nil {
				return trip.Trip{}, err
			}
		}

		var updated trip.Trip

		row := tx.QueryRow(
			ctx,
			`UPDATE trips
			 SET status = 'cancelled',
			     cancellation_reason = NULLIF($2, ''),
			     cancelled_by = $3,
			     rider_no_show = $4,
			     cancelled_at = CURRENT_TIMESTAMP,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1
			 RETURNING `+tripColumns,
			record.TripID,
			record.Reason,
			string(record.By),
			record.RiderNoShow,
		)

		if err := scanTrip(row, &updated); err != nil {
			return trip.Trip{}, err
		}

		return updated, writeOutboxEvent(ctx, tx, "trip.cancelled", updated.ID, map[string]string{
			"trip_id":       updated.ID,
			"reason":        record.Reason,
			"cancelled_by":  string(record.By),
			"rider_no_show": strconv.FormatBool(record.RiderNoShow),
		})
	})
}

// MarkArrived sets arrived_at on an accepted trip that has none, with its
// trip.driver_arrived event, in one statement and transaction.
func (r *TripRepository) MarkArrived(ctx context.Context, tripID string) (trip.Trip, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var updated trip.Trip

	err = scanTrip(tx.QueryRow(
		ctx,
		`UPDATE trips
		 SET arrived_at = CURRENT_TIMESTAMP,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'accepted' AND arrived_at IS NULL
		 RETURNING `+tripColumns,
		tripID,
	), &updated)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		current, findErr := r.FindByID(ctx, tripID)
		if findErr != nil {
			return trip.Trip{}, findErr
		}

		if current.Status == trip.StatusAccepted && current.ArrivedAt != nil {
			return current, nil
		}

		return trip.Trip{}, trip.ErrInvalidTransition
	case err != nil:
		return trip.Trip{}, fmt.Errorf("mark arrival: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, "trip.driver_arrived", updated.ID, map[string]string{
		"trip_id":   updated.ID,
		"rider_id":  updated.RiderID,
		"driver_id": updated.DriverID,
	}); err != nil {
		return trip.Trip{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return trip.Trip{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}

// transition is the shared skeleton for every state change: open a
// transaction, lock and read the current row, verify the move is legal
// per the domain's Status.CanTransitionTo, run the caller-supplied SQL,
// write the outbox event, and commit. Centralizing this means the
// "is this transition allowed" check can never be forgotten in a new
// use case, and every transition gets the same atomicity guarantee.
func (r *TripRepository) transition(
	ctx context.Context,
	tripID string,
	target trip.Status,
	apply func(tx pgx.Tx, current trip.Trip) (trip.Trip, error),
) (trip.Trip, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current trip.Trip

	lockRow := tx.QueryRow(
		ctx,
		`SELECT `+tripColumns+`
		 FROM trips
		 WHERE id = $1
		 FOR UPDATE`,
		tripID,
	)

	if err := scanTrip(lockRow, &current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Trip{}, trip.ErrTripNotFound
		}

		return trip.Trip{}, fmt.Errorf("lock trip: %w", err)
	}

	if !current.Status.CanTransitionTo(target) {
		return trip.Trip{}, trip.ErrInvalidTransition
	}

	updated, err := apply(tx, current)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("apply trip transition: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return trip.Trip{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}

func writeOutboxEvent(
	ctx context.Context,
	tx pgx.Tx,
	eventType string,
	aggregateID string,
	payload map[string]string,
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	now := time.Now().UTC()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"trip",
		aggregateID,
		eventType,
		schemaVersion,
		payloadJSON,
		now,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}

	return nil
}

// tripColumns is the column list scanTrip reads, in its order.
const tripColumns = `id, rider_id, driver_id, status,
	pickup_latitude, pickup_longitude,
	dropoff_latitude, dropoff_longitude,
	cancellation_reason,
	vehicle_class,
	payment_method,
	requested_at, accepted_at, started_at, completed_at, cancelled_at,
	created_at, updated_at,
	pickup_address, dropoff_address, pickup_details, pickup_note,
	COALESCE(pickup_photo_media_id::text, ''),
	COALESCE(quote_id::text, ''), COALESCE(quoted_fare::text, ''), COALESCE(currency_code, ''),
	arrived_at, COALESCE(cancelled_by, ''), rider_no_show,
	passenger_name, passenger_phone, scheduled, stops`

func scanTrip(row pgx.Row, dest *trip.Trip) error {
	var status string
	var driverID, cancellationReason *string
	var quotedFare, cancelledBy string
	var stops []byte

	err := row.Scan(
		&dest.ID,
		&dest.RiderID,
		&driverID,
		&status,
		&dest.Pickup.Latitude,
		&dest.Pickup.Longitude,
		&dest.Dropoff.Latitude,
		&dest.Dropoff.Longitude,
		&cancellationReason,
		&dest.VehicleClass,
		&dest.PaymentMethod,
		&dest.RequestedAt,
		&dest.AcceptedAt,
		&dest.StartedAt,
		&dest.CompletedAt,
		&dest.CancelledAt,
		&dest.CreatedAt,
		&dest.UpdatedAt,
		&dest.PickupAddress,
		&dest.DropoffAddress,
		&dest.PickupDetails,
		&dest.PickupNote,
		&dest.PickupPhotoMediaID,
		&dest.QuoteID,
		&quotedFare,
		&dest.CurrencyCode,
		&dest.ArrivedAt,
		&cancelledBy,
		&dest.RiderNoShow,
		&dest.PassengerName,
		&dest.PassengerPhone,
		&dest.Scheduled,
		&stops,
	)
	if err != nil {
		return err
	}

	decoded, err := decodeStops(stops)
	if err != nil {
		return err
	}

	dest.Stops = decoded

	dest.Status = trip.Status(status)
	dest.QuotedFare = trimDecimal(quotedFare)
	dest.CancelledBy = trip.CancelledBy(cancelledBy)

	if driverID != nil {
		dest.DriverID = *driverID
	}

	if cancellationReason != nil {
		dest.CancellationReason = *cancellationReason
	}

	return nil
}

// trimDecimal shows a NUMERIC(12, 4) amount without the trailing zeros of
// its scale: "4500.0000" as "4500", "4500.5000" as "4500.5".
func trimDecimal(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}

	value = strings.TrimRight(value, "0")

	return strings.TrimSuffix(value, ".")
}
