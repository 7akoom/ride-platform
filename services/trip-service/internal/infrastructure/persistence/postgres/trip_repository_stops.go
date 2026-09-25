package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// storedStop is one stop as the stops columns keep it.
type storedStop struct {
	Latitude  float64    `json:"latitude"`
	Longitude float64    `json:"longitude"`
	Address   string     `json:"address"`
	ReachedAt *time.Time `json:"reached_at"`
}

func encodeStops(stops []trip.Stop) ([]byte, error) {
	stored := make([]storedStop, 0, len(stops))

	for _, stop := range stops {
		stored = append(stored, storedStop{
			Latitude:  stop.Coordinates.Latitude,
			Longitude: stop.Coordinates.Longitude,
			Address:   stop.Address,
			ReachedAt: stop.ReachedAt,
		})
	}

	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("encode stops: %w", err)
	}

	return encoded, nil
}

func decodeStops(raw []byte) ([]trip.Stop, error) {
	var stored []storedStop
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode stops: %w", err)
	}

	var stops []trip.Stop

	for _, stop := range stored {
		stops = append(stops, trip.Stop{
			Coordinates: trip.Coordinates{Latitude: stop.Latitude, Longitude: stop.Longitude},
			Address:     stop.Address,
			ReachedAt:   stop.ReachedAt,
		})
	}

	return stops, nil
}

// MarkStopReached records that the driver is at one of the stops of a trip in
// progress, with its trip.stop_reached event. See trip.StopStore.
func (r *TripRepository) MarkStopReached(ctx context.Context, tripID string, position int) (trip.Trip, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current trip.Trip

	err = scanTrip(tx.QueryRow(ctx, `SELECT `+tripColumns+` FROM trips WHERE id = $1 FOR UPDATE`, tripID), &current)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return trip.Trip{}, trip.ErrTripNotFound
	case err != nil:
		return trip.Trip{}, fmt.Errorf("lock trip: %w", err)
	}

	if position < 1 || position > len(current.Stops) {
		return trip.Trip{}, trip.ErrStopNotFound
	}

	if current.Status != trip.StatusInProgress {
		return trip.Trip{}, trip.ErrInvalidTransition
	}

	if current.Stops[position-1].ReachedAt != nil {
		return current, nil
	}

	var updated trip.Trip

	if err := scanTrip(tx.QueryRow(
		ctx,
		`UPDATE trips
		 SET stops = jsonb_set(stops, ARRAY[($2 - 1)::text, 'reached_at'], to_jsonb(CURRENT_TIMESTAMP)),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING `+tripColumns,
		tripID, position,
	), &updated); err != nil {
		return trip.Trip{}, fmt.Errorf("mark stop reached: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, "trip.stop_reached", updated.ID, map[string]string{
		"trip_id":   updated.ID,
		"rider_id":  updated.RiderID,
		"driver_id": updated.DriverID,
		"position":  fmt.Sprint(position),
	}); err != nil {
		return trip.Trip{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return trip.Trip{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}
