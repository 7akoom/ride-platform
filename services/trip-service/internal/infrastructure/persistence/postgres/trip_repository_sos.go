package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func (r *TripRepository) TriggerSOS(
	ctx context.Context,
	tripID string,
	triggeredBy trip.SosTriggeredBy,
	location trip.Coordinates,
) (string, time.Time, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// TriggerSOS isn't a status transition (see the Repository
	// interface comment), so this only needs to confirm the trip
	// exists — not lock or check its current status the way
	// transition() does for Accept/Start/Complete/Cancel.
	var exists bool

	if err := tx.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM trips WHERE id = $1)`,
		tripID,
	).Scan(&exists); err != nil {
		return "", time.Time{}, fmt.Errorf("check trip exists: %w", err)
	}

	if !exists {
		return "", time.Time{}, trip.ErrTripNotFound
	}

	var alertID string
	var triggeredAt time.Time

	row := tx.QueryRow(
		ctx,
		`INSERT INTO sos_alerts (trip_id, triggered_by, latitude, longitude)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		tripID,
		string(triggeredBy),
		location.Latitude,
		location.Longitude,
	)

	if err := row.Scan(&alertID, &triggeredAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			// A CHECK constraint failure here can only be the
			// triggered_by guard — everything else is validated
			// before this INSERT runs.
			return "", time.Time{}, trip.ErrInvalidSosTriggeredBy
		}

		return "", time.Time{}, fmt.Errorf("insert sos alert: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, "trip.sos_triggered", tripID, map[string]string{
		"trip_id":      tripID,
		"alert_id":     alertID,
		"triggered_by": string(triggeredBy),
		"latitude":     strconv.FormatFloat(location.Latitude, 'f', -1, 64),
		"longitude":    strconv.FormatFloat(location.Longitude, 'f', -1, 64),
	}); err != nil {
		return "", time.Time{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("commit transaction: %w", err)
	}

	return alertID, triggeredAt, nil
}
