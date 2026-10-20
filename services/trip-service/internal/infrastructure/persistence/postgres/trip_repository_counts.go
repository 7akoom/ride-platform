package postgres

import (
	"context"
	"fmt"
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
