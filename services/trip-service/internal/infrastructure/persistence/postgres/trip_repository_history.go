package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/feed"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// historyColumns is the column list scanTrip reads, in its order.
const historyColumns = tripColumns

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

// RecentDestinations returns where the rider's completed trips ended, newest
// first. Drop-offs within about 10 m (4 decimal places) count as one place;
// each place keeps the address and time of its latest trip. See
// trip.DestinationStore.
func (r *TripRepository) RecentDestinations(
	ctx context.Context,
	riderID string,
	limit int,
) ([]trip.Destination, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT latitude, longitude, address, completed_at
		 FROM (
		     SELECT DISTINCT ON (round(dropoff_latitude::numeric, 4), round(dropoff_longitude::numeric, 4))
		            dropoff_latitude AS latitude, dropoff_longitude AS longitude,
		            dropoff_address AS address, completed_at
		     FROM trips
		     WHERE rider_id = $1 AND status = 'completed'
		     ORDER BY round(dropoff_latitude::numeric, 4), round(dropoff_longitude::numeric, 4),
		              completed_at DESC
		 ) places
		 ORDER BY completed_at DESC
		 LIMIT $2`,
		riderID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent destinations: %w", err)
	}
	defer rows.Close()

	out := make([]trip.Destination, 0, limit)

	for rows.Next() {
		var d trip.Destination

		if err := rows.Scan(&d.Coordinates.Latitude, &d.Coordinates.Longitude, &d.Address, &d.LastTripAt); err != nil {
			return nil, fmt.Errorf("scan recent destination: %w", err)
		}

		out = append(out, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent destinations: %w", err)
	}

	return out, nil
}

// ListTripsBefore lists a profile's trips older than before (nil: the newest)
// by (requested_at, id), newest first, for the activity page. The id compares
// in byte order (COLLATE "C"), the order the page merges in. See feed.Trips.
func (r *TripRepository) ListTripsBefore(
	ctx context.Context,
	owner feed.Owner,
	ownerID string,
	before *feed.Cursor,
	limit int,
) ([]trip.Trip, error) {
	column := "rider_id"
	if owner == feed.OwnerDriver {
		column = "driver_id"
	}

	query := `SELECT ` + historyColumns + ` FROM trips WHERE ` + column + ` = $1`
	args := []any{ownerID}

	if before != nil {
		args = append(args, before.At, before.ID)
		query += ` AND (requested_at < $2 OR (requested_at = $2 AND id::text COLLATE "C" < $3))`
	}

	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY requested_at DESC, id::text COLLATE "C" DESC LIMIT $%d`, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trips for the activity page: %w", err)
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

	return trips, rows.Err()
}
