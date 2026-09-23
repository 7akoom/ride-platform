package trip

import (
	"context"
	"errors"
	"strings"
)

// arrivalRadiusMeters is how close to the pickup a driver must be to mark
// arrival: the waiting fee and the no-show count from there, so it is not
// the driver's word alone.
const arrivalRadiusMeters = 200

// ArrivalStore records a driver's arrival (the repository).
type ArrivalStore interface {
	MarkArrived(ctx context.Context, tripID string) (Trip, error)
}

// WithDriverArrival adds MarkDriverArrived: the driver of an accepted trip
// says they are at the pickup, and their last reported position must agree.
func WithDriverArrival(base Service, locator DriverLocator, store ArrivalStore) Service {
	if base == nil {
		panic("trip service is required")
	}

	if locator == nil {
		panic("driver locator is required")
	}

	if store == nil {
		panic("arrival store is required")
	}

	return &arrivalService{Service: base, locator: locator, store: store}
}

type arrivalService struct {
	Service

	locator DriverLocator
	store   ArrivalStore
}

func (s *arrivalService) Unwrap() Service { return s.Service }

func (s *arrivalService) MarkDriverArrived(ctx context.Context, tripID string) (Trip, error) {
	tripID = strings.TrimSpace(tripID)
	if tripID == "" {
		return Trip{}, ErrTripIDRequired
	}

	current, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return Trip{}, err
	}

	if current.Status != StatusAccepted || current.DriverID == "" {
		return Trip{}, ErrInvalidTransition
	}

	if current.ArrivedAt != nil {
		return current, nil
	}

	position, err := s.locator.DriverLocation(ctx, current.DriverID)
	if errors.Is(err, ErrDriverLocationUnavailable) {
		return Trip{}, ErrArrivalPositionUnknown
	}

	if err != nil {
		return Trip{}, err
	}

	here := Coordinates{Latitude: position.Latitude, Longitude: position.Longitude}
	if distanceMeters(here, current.Pickup) > arrivalRadiusMeters {
		return Trip{}, ErrTooFarFromPickup
	}

	return s.store.MarkArrived(ctx, tripID)
}
