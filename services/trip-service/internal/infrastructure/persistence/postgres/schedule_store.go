package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/schedule"
)

// ScheduleStore keeps trips booked ahead.
type ScheduleStore struct {
	pool *pgxpool.Pool
}

var _ schedule.Store = (*ScheduleStore)(nil)

func NewScheduleStore(pool *pgxpool.Pool) *ScheduleStore {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &ScheduleStore{pool: pool}
}

var scheduleIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const rideColumns = `id, rider_id, idempotency_key, status, scheduled_at, time_zone,
        pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
        pickup_address, dropoff_address,
        COALESCE(pickup_saved_address_id::text, ''), COALESCE(dropoff_saved_address_id::text, ''),
        vehicle_class, payment_method, passenger_name, passenger_phone,
        next_attempt_at, attempts, last_error,
        COALESCE(trip_id::text, ''), created_at, dispatched_at, cancelled_at, failed_at`

func scanRide(row pgx.Row) (schedule.Ride, error) {
	var (
		r      schedule.Ride
		status string
	)

	err := row.Scan(
		&r.ID, &r.RiderID, &r.IdempotencyKey, &status, &r.ScheduledAt, &r.TimeZone,
		&r.Pickup.Latitude, &r.Pickup.Longitude, &r.Dropoff.Latitude, &r.Dropoff.Longitude,
		&r.PickupAddress, &r.DropoffAddress,
		&r.PickupSavedAddressID, &r.DropoffSavedAddressID,
		&r.VehicleClass, &r.PaymentMethod, &r.PassengerName, &r.PassengerPhone,
		&r.NextAttemptAt, &r.Attempts, &r.LastError,
		&r.TripID, &r.CreatedAt, &r.DispatchedAt, &r.CancelledAt, &r.FailedAt,
	)
	r.Status = schedule.Status(status)

	return r, err
}

func (s *ScheduleStore) Create(ctx context.Context, ride schedule.Ride, maxUpcoming int) (schedule.Ride, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return schedule.Ride{}, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The rider's bookings are counted and added one at a time.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('scheduled_trips:' || $1))`, ride.RiderID); err != nil {
		return schedule.Ride{}, false, fmt.Errorf("lock the rider's bookings: %w", err)
	}

	existing, err := scanRide(tx.QueryRow(
		ctx,
		`SELECT `+rideColumns+` FROM scheduled_trips WHERE rider_id = $1 AND idempotency_key = $2`,
		ride.RiderID, ride.IdempotencyKey,
	))

	switch {
	case err == nil:
		return existing, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return schedule.Ride{}, false, fmt.Errorf("select booking: %w", err)
	}

	var upcoming int
	if err := tx.QueryRow(
		ctx,
		`SELECT count(*) FROM scheduled_trips WHERE rider_id = $1 AND status = 'scheduled'`,
		ride.RiderID,
	).Scan(&upcoming); err != nil {
		return schedule.Ride{}, false, fmt.Errorf("count upcoming bookings: %w", err)
	}

	if upcoming >= maxUpcoming {
		return schedule.Ride{}, false, schedule.ErrTooManyUpcoming
	}

	created, err := scanRide(tx.QueryRow(
		ctx,
		`INSERT INTO scheduled_trips
		    (id, rider_id, idempotency_key, scheduled_at, time_zone,
		     pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
		     pickup_address, dropoff_address, pickup_saved_address_id, dropoff_saved_address_id,
		     vehicle_class, payment_method, passenger_name, passenger_phone, next_attempt_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		         NULLIF($12::text, '')::uuid, NULLIF($13::text, '')::uuid, $14, $15, $16, $17, $18)
		 RETURNING `+rideColumns,
		ride.ID, ride.RiderID, ride.IdempotencyKey, ride.ScheduledAt, ride.TimeZone,
		ride.Pickup.Latitude, ride.Pickup.Longitude, ride.Dropoff.Latitude, ride.Dropoff.Longitude,
		ride.PickupAddress, ride.DropoffAddress, ride.PickupSavedAddressID, ride.DropoffSavedAddressID,
		ride.VehicleClass, ride.PaymentMethod, ride.PassengerName, ride.PassengerPhone, ride.NextAttemptAt,
	))
	if err != nil {
		return schedule.Ride{}, false, fmt.Errorf("insert booking: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return schedule.Ride{}, false, fmt.Errorf("commit transaction: %w", err)
	}

	return created, false, nil
}

func (s *ScheduleStore) FindByKey(ctx context.Context, riderID, key string) (schedule.Ride, bool, error) {
	found, err := scanRide(s.pool.QueryRow(
		ctx,
		`SELECT `+rideColumns+` FROM scheduled_trips WHERE rider_id = $1 AND idempotency_key = $2`,
		riderID, key,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return schedule.Ride{}, false, nil
	case err != nil:
		return schedule.Ride{}, false, fmt.Errorf("select booking: %w", err)
	}

	return found, true, nil
}

func (s *ScheduleStore) ListForRider(ctx context.Context, riderID string, includePast bool, limit int) ([]schedule.Ride, error) {
	query := `SELECT ` + rideColumns + ` FROM scheduled_trips
	          WHERE rider_id = $1 AND status = 'scheduled'
	          ORDER BY scheduled_at LIMIT $2`
	if includePast {
		query = `SELECT ` + rideColumns + ` FROM scheduled_trips
		         WHERE rider_id = $1
		         ORDER BY scheduled_at DESC LIMIT $2`
	}

	rows, err := s.pool.Query(ctx, query, riderID, limit)
	if err != nil {
		return nil, fmt.Errorf("select bookings: %w", err)
	}

	rides, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (schedule.Ride, error) { return scanRide(row) })
	if err != nil {
		return nil, fmt.Errorf("read bookings: %w", err)
	}

	return rides, nil
}

func (s *ScheduleStore) Cancel(ctx context.Context, id, riderID string, now time.Time) (schedule.Ride, error) {
	if !scheduleIDPattern.MatchString(id) {
		return schedule.Ride{}, schedule.ErrNotFound
	}

	cancelled, err := scanRide(s.pool.QueryRow(
		ctx,
		`UPDATE scheduled_trips SET status = 'cancelled', cancelled_at = $3
		 WHERE id = $1 AND rider_id = $2 AND status = 'scheduled'
		 RETURNING `+rideColumns,
		id, riderID, now,
	))
	if err == nil {
		return cancelled, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return schedule.Ride{}, fmt.Errorf("cancel booking: %w", err)
	}

	var exists bool
	if err := s.pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM scheduled_trips WHERE id = $1 AND rider_id = $2)`,
		id, riderID,
	).Scan(&exists); err != nil {
		return schedule.Ride{}, fmt.Errorf("look for the booking: %w", err)
	}

	if exists {
		return schedule.Ride{}, schedule.ErrNotScheduled
	}

	return schedule.Ride{}, schedule.ErrNotFound
}

func (s *ScheduleStore) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]schedule.Ride, error) {
	rows, err := s.pool.Query(
		ctx,
		`UPDATE scheduled_trips SET next_attempt_at = $2, attempts = attempts + 1
		 WHERE id IN (
		     SELECT id FROM scheduled_trips
		     WHERE status = 'scheduled' AND next_attempt_at <= $1
		     ORDER BY next_attempt_at
		     LIMIT $3
		     FOR UPDATE SKIP LOCKED
		 )
		 RETURNING `+rideColumns,
		now, now.Add(lease), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim due bookings: %w", err)
	}

	rides, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (schedule.Ride, error) { return scanRide(row) })
	if err != nil {
		return nil, fmt.Errorf("read due bookings: %w", err)
	}

	return rides, nil
}

func (s *ScheduleStore) MarkDispatched(ctx context.Context, id, tripID string, now time.Time) error {
	return s.whileScheduled(ctx, s.pool,
		`UPDATE scheduled_trips SET status = 'dispatched', trip_id = $2, dispatched_at = $3
		 WHERE id = $1 AND status = 'scheduled'`,
		id, tripID, now)
}

func (s *ScheduleStore) Retry(ctx context.Context, id, reason string, next time.Time) error {
	return s.whileScheduled(ctx, s.pool,
		`UPDATE scheduled_trips SET last_error = left($2, 300), next_attempt_at = $3
		 WHERE id = $1 AND status = 'scheduled'`,
		id, reason, next)
}

func (s *ScheduleStore) Fail(ctx context.Context, id, reason string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var riderID string

	err = tx.QueryRow(
		ctx,
		`UPDATE scheduled_trips SET status = 'failed', failed_at = $3, last_error = left($2, 300)
		 WHERE id = $1 AND status = 'scheduled'
		 RETURNING rider_id`,
		id, reason, now,
	).Scan(&riderID)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return schedule.ErrNotScheduled
	case err != nil:
		return fmt.Errorf("fail booking: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, "trip.schedule_failed", id, map[string]string{
		"scheduled_trip_id": id,
		"rider_id":          riderID,
		"reason":            reason,
	}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// whileScheduled runs an update meant for a booking still scheduled;
// ErrNotScheduled when it no longer is.
func (s *ScheduleStore) whileScheduled(ctx context.Context, db execer, sql string, args ...any) error {
	tag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update booking: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return schedule.ErrNotScheduled
	}

	return nil
}
