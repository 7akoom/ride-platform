package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) AcceptTrip(
	ctx context.Context,
	tripID string,
	driverID string,
) (Trip, error) {
	trimmedTripID := strings.TrimSpace(tripID)
	if trimmedTripID == "" {
		return Trip{}, ErrTripIDRequired
	}

	trimmedDriverID := strings.TrimSpace(driverID)
	if trimmedDriverID == "" {
		return Trip{}, ErrDriverIDRequired
	}

	_, err := s.repository.FindActiveByDriverID(ctx, trimmedDriverID)
	switch {
	case err == nil:
		return Trip{}, ErrDriverHasActiveTrip
	case errors.Is(err, ErrTripNotFound):
		// Expected path: driver has no active trip right now.
	default:
		return Trip{}, fmt.Errorf("check driver's active trip: %w", err)
	}

	accepted, err := s.repository.Accept(ctx, trimmedTripID, trimmedDriverID)
	if err != nil {
		return Trip{}, fmt.Errorf("accept trip: %w", err)
	}

	return accepted, nil
}
