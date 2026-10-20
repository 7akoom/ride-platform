package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CountRiderCompletedTrips counts the rider's completed trips (trips_rider_id_idx).
// See trip.TripCounter.
func (r *TripRepository) CountRiderCompletedTrips(ctx context.Context, riderID string) (int, error) {
	var count int

	err := r.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM trips WHERE rider_id = $1 AND status = 'completed'`,
		riderID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count rider completed trips: %w", err)
	}

	return count, nil
}

// CountDriverCompletedTrips counts the driver's completed trips
// (trips_driver_completed_idx). See trip.TripCounter.
func (r *TripRepository) CountDriverCompletedTrips(ctx context.Context, driverID string) (int, error) {
	var count int

	err := r.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM trips WHERE driver_id = $1 AND status = 'completed'`,
		driverID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count driver completed trips: %w", err)
	}

	return count, nil
}

// ListWaitingAtPickup lists the trips whose captain is at the pickup since before
// arrivedBefore, the longest waiting first. See trip.WaitingTrips.
func (r *TripRepository) ListWaitingAtPickup(ctx context.Context, arrivedBefore time.Time, limit int) ([]string, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id::text FROM trips
		 WHERE status = 'accepted' AND arrived_at IS NOT NULL AND arrived_at <= $1
		 ORDER BY arrived_at
		 LIMIT $2`,
		arrivedBefore,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select trips waiting at the pickup: %w", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read trips waiting at the pickup: %w", err)
	}

	return ids, nil
}
