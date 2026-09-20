package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// historyColumns is the column list scanTrip reads, in its order.
const historyColumns = `id, rider_id, driver_id, status,
                pickup_latitude, pickup_longitude,
                dropoff_latitude, dropoff_longitude,
                cancellation_reason,
                vehicle_class,
                payment_method,
                requested_at, accepted_at, started_at, completed_at, cancelled_at,
                created_at, updated_at`

// ListByRiderID returns the rider's trips, newest first. See trip.HistoryStore.
func (r *TripRepository) ListByRiderID(
	ctx context.Context,
	riderID string,
	afterTripID string,
	limit int,
) ([]trip.Trip, error) {
	return r.listOwned(ctx, "rider_id", riderID, afterTripID, limit)
}

// ListByDriverID returns the driver's trips, newest first. See trip.HistoryStore.
func (r *TripRepository) ListByDriverID(
	ctx context.Context,
	driverID string,
	afterTripID string,
	limit int,
) ([]trip.Trip, error) {
	return r.listOwned(ctx, "driver_id", driverID, afterTripID, limit)
}

func (r *TripRepository) listOwned(
	ctx context.Context,
	ownerColumn string,
	ownerID string,
	afterTripID string,
	limit int,
) ([]trip.Trip, error) {
	args := []any{ownerID, limit}
	if afterTripID != "" {
		args = []any{ownerID, afterTripID, limit}
	}

	rows, err := r.pool.Query(ctx, historyQuery(ownerColumn, afterTripID != ""), args...)
	if err != nil {
		return nil, fmt.Errorf("query trip history: %w", err)
	}
	defer rows.Close()

	trips := []trip.Trip{}

	for rows.Next() {
		var found trip.Trip

		if err := scanTrip(rows, &found); err != nil {
			return nil, fmt.Errorf("scan trip: %w", err)
		}

		trips = append(trips, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read trip history: %w", err)
	}

	return trips, nil
}

// historyQuery orders by (requested_at, id), newest first, and continues after
// the trip whose id is $2. That trip must be the owner's own (the subquery repeats
// the owner condition), so a page token taken from somebody else's history yields
// nothing instead of a position in it. ownerColumn is chosen by the caller from a
// fixed set and never comes from a request.
func historyQuery(ownerColumn string, afterCursor bool) string {
	switch ownerColumn {
	case "rider_id", "driver_id":
	default:
		panic("unsupported history owner column: " + ownerColumn)
	}

	if !afterCursor {
		return `SELECT ` + historyColumns + `
                FROM trips
                WHERE ` + ownerColumn + ` = $1
                ORDER BY requested_at DESC, id DESC
                LIMIT $2`
	}

	return `SELECT ` + historyColumns + `
            FROM trips
            WHERE ` + ownerColumn + ` = $1
              AND (requested_at, id) < (
                    SELECT requested_at, id FROM trips
                    WHERE id = $2 AND ` + ownerColumn + ` = $1
                  )
            ORDER BY requested_at DESC, id DESC
            LIMIT $3`
}
