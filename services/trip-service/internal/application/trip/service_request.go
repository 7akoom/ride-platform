package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) RequestTrip(
	ctx context.Context,
	input RequestTripInput,
) (Trip, error) {
	riderID := strings.TrimSpace(input.RiderID)
	if riderID == "" {
		return Trip{}, ErrRiderIDRequired
	}

	pickup, err := NewCoordinates(input.PickupLat, input.PickupLng)
	if err != nil {
		return Trip{}, err
	}

	dropoff, err := NewCoordinates(input.DropoffLat, input.DropoffLng)
	if err != nil {
		return Trip{}, err
	}

	_, err = s.repository.FindActiveByRiderID(ctx, riderID)
	switch {
	case err == nil:
		return Trip{}, ErrRiderHasActiveTrip
	case errors.Is(err, ErrTripNotFound):
		// Expected path: rider has no active trip right now.
	default:
		return Trip{}, fmt.Errorf("check rider's active trip: %w", err)
	}

	created, err := s.repository.Create(
		ctx,
		CreateInput{
			ID:      s.idGenerator.NewID(),
			RiderID: riderID,
			Pickup:  pickup,
			Dropoff: dropoff,
		},
	)
	if err != nil {
		return Trip{}, fmt.Errorf("create trip: %w", err)
	}

	return created, nil
}
