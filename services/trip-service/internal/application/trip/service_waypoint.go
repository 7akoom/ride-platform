package trip

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WaypointMinInterval is how far apart two recorded points for the same
// trip must be — a point every 15-30s is plenty to reconstruct a route
// after the fact; every single GPS ping (every 2-5s, matching
// location-service's driver ping interval) would be needless volume
// for no real benefit to the safety use case this exists for.
const WaypointMinInterval = 15 * time.Second

func (s *service) RecordWaypoint(
	ctx context.Context,
	tripID string,
	latitude, longitude float64,
) error {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return ErrTripIDRequired
	}

	location, err := NewCoordinates(latitude, longitude)
	if err != nil {
		return err
	}

	if err := s.repository.RecordWaypointIfDue(
		ctx,
		trimmedID,
		location,
		time.Now().UTC(),
		WaypointMinInterval,
	); err != nil {
		return fmt.Errorf("record waypoint: %w", err)
	}

	return nil
}

func (s *service) GetTripPath(
	ctx context.Context,
	tripID string,
) ([]Waypoint, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return nil, ErrTripIDRequired
	}

	waypoints, err := s.repository.ListWaypoints(ctx, trimmedID)
	if err != nil {
		return nil, fmt.Errorf("list waypoints: %w", err)
	}

	return waypoints, nil
}
