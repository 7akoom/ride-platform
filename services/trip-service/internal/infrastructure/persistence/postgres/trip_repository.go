package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	row := tx.QueryRow(
		ctx,
		`INSERT INTO trips
		    (id, rider_id, pickup_latitude, pickup_longitude,
		     dropoff_latitude, dropoff_longitude)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, rider_id, driver_id, status,
		           pickup_latitude, pickup_longitude,
		           dropoff_latitude, dropoff_longitude,
		           cancellation_reason,
		           requested_at, accepted_at, started_at, completed_at, cancelled_at,
		           created_at, updated_at`,
		input.ID,
		input.RiderID,
		input.Pickup.Latitude,
		input.Pickup.Longitude,
		input.Dropoff.Latitude,
		input.Dropoff.Longitude,
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
	query := `SELECT id, rider_id, driver_id, status,
	                 pickup_latitude, pickup_longitude,
	                 dropoff_latitude, dropoff_longitude,
	                 cancellation_reason,
	                 requested_at, accepted_at, started_at, completed_at, cancelled_at,
	                 created_at, updated_at
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
			 RETURNING id, rider_id, driver_id, status,
			           pickup_latitude, pickup_longitude,
			           dropoff_latitude, dropoff_longitude,
			           cancellation_reason,
			           requested_at, accepted_at, started_at, completed_at, cancelled_at,
			           created_at, updated_at`,
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
			 RETURNING id, rider_id, driver_id, status,
			           pickup_latitude, pickup_longitude,
			           dropoff_latitude, dropoff_longitude,
			           cancellation_reason,
			           requested_at, accepted_at, started_at, completed_at, cancelled_at,
			           created_at, updated_at`,
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
			 RETURNING id, rider_id, driver_id, status,
			           pickup_latitude, pickup_longitude,
			           dropoff_latitude, dropoff_longitude,
			           cancellation_reason,
			           requested_at, accepted_at, started_at, completed_at, cancelled_at,
			           created_at, updated_at`,
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
	tripID string,
	reason string,
) (trip.Trip, error) {
	return r.transition(ctx, tripID, trip.StatusCancelled, func(tx pgx.Tx, current trip.Trip) (trip.Trip, error) {
		var updated trip.Trip

		row := tx.QueryRow(
			ctx,
			`UPDATE trips
			 SET status = 'cancelled',
			     cancellation_reason = NULLIF($2, ''),
			     cancelled_at = CURRENT_TIMESTAMP,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1
			 RETURNING id, rider_id, driver_id, status,
			           pickup_latitude, pickup_longitude,
			           dropoff_latitude, dropoff_longitude,
			           cancellation_reason,
			           requested_at, accepted_at, started_at, completed_at, cancelled_at,
			           created_at, updated_at`,
			tripID,
			reason,
		)

		if err := scanTrip(row, &updated); err != nil {
			return trip.Trip{}, err
		}

		return updated, writeOutboxEvent(ctx, tx, "trip.cancelled", updated.ID, map[string]string{
			"trip_id": updated.ID,
			"reason":  reason,
		})
	})
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
		`SELECT id, rider_id, driver_id, status,
		        pickup_latitude, pickup_longitude,
		        dropoff_latitude, dropoff_longitude,
		        cancellation_reason,
		        requested_at, accepted_at, started_at, completed_at, cancelled_at,
		        created_at, updated_at
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

func scanTrip(row pgx.Row, dest *trip.Trip) error {
	var status string
	var driverID, cancellationReason *string

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
		&dest.RequestedAt,
		&dest.AcceptedAt,
		&dest.StartedAt,
		&dest.CompletedAt,
		&dest.CancelledAt,
		&dest.CreatedAt,
		&dest.UpdatedAt,
	)
	if err != nil {
		return err
	}

	dest.Status = trip.Status(status)

	if driverID != nil {
		dest.DriverID = *driverID
	}

	if cancellationReason != nil {
		dest.CancellationReason = *cancellationReason
	}

	return nil
}
