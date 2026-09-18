package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// RecordWaypointIfDue inserts one point, but only if at least
// minInterval has passed since the most recently recorded point for
// this trip (or none exists yet). The throttle is expressed as a single
// guarded INSERT rather than a read-then-write, so two concurrent calls
// for the same trip can't both slip through the check.
func (r *TripRepository) RecordWaypointIfDue(
	ctx context.Context,
	tripID string,
	location trip.Coordinates,
	recordedAt time.Time,
	minInterval time.Duration,
) error {
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO trip_waypoints (trip_id, latitude, longitude, recorded_at)
		 SELECT $1, $2, $3, $4
		 WHERE $4::timestamptz - COALESCE(
		     (SELECT MAX(recorded_at) FROM trip_waypoints WHERE trip_id = $1),
		     to_timestamp(0)
		 ) >= make_interval(secs => $5)`,
		tripID,
		location.Latitude,
		location.Longitude,
		recordedAt,
		minInterval.Seconds(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// Foreign key violation: trip_id doesn't reference a real
			// trip.
			return trip.ErrTripNotFound
		}

		return fmt.Errorf("insert trip waypoint: %w", err)
	}

	return nil
}

func (r *TripRepository) ListWaypoints(
	ctx context.Context,
	tripID string,
) ([]trip.Waypoint, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT latitude, longitude, recorded_at
		 FROM trip_waypoints
		 WHERE trip_id = $1
		 ORDER BY recorded_at ASC`,
		tripID,
	)
	if err != nil {
		return nil, fmt.Errorf("query trip waypoints: %w", err)
	}
	defer rows.Close()

	waypoints := make([]trip.Waypoint, 0)

	for rows.Next() {
		var (
			latitude, longitude float64
			recordedAt          time.Time
		)

		if err := rows.Scan(&latitude, &longitude, &recordedAt); err != nil {
			return nil, fmt.Errorf("scan trip waypoint: %w", err)
		}

		waypoints = append(waypoints, trip.Waypoint{
			Coordinates: trip.Coordinates{Latitude: latitude, Longitude: longitude},
			RecordedAt:  recordedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trip waypoints: %w", err)
	}

	return waypoints, nil
}
