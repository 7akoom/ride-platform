package trip

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxStops is how many stops a trip may make between pickup and dropoff.
const MaxStops = 2

// Stop is a place the trip stops on the way, in order. ReachedAt is when the
// driver marked it (nil until then).
type Stop struct {
	Coordinates Coordinates
	// As the rider picked it; at most 300 characters.
	Address   string
	ReachedAt *time.Time
}

// NormalizeStops checks a trip's stops: at most MaxStops, real points,
// addresses short enough to keep. What the caller says about reaching them is
// dropped.
func NormalizeStops(stops []Stop) ([]Stop, error) {
	if len(stops) > MaxStops {
		return nil, ErrTooManyStops
	}

	out := make([]Stop, 0, len(stops))

	for _, stop := range stops {
		point, err := NewCoordinates(stop.Coordinates.Latitude, stop.Coordinates.Longitude)
		if err != nil {
			return nil, err
		}

		address := strings.TrimSpace(stop.Address)
		if utf8.RuneCountInString(address) > maxAddressLength {
			return nil, ErrAddressTooLong
		}

		out = append(out, Stop{Coordinates: point, Address: address})
	}

	return out, nil
}

// StopStore records that the driver reached a stop (the repository).
type StopStore interface {
	// MarkStopReached sets the stop's ReachedAt (position from 1) of a trip in
	// progress, with its trip.stop_reached event. A stop already reached is
	// returned unchanged. ErrInvalidTransition when the trip is not in
	// progress, ErrStopNotFound when it has no such stop.
	MarkStopReached(ctx context.Context, tripID string, position int) (Trip, error)
}

// WithStopArrivals adds ReachStop: the driver of a trip in progress says they
// are at one of its stops, and their last reported position must agree.
func WithStopArrivals(base Service, locator DriverLocator, store StopStore) Service {
	if base == nil {
		panic("trip service is required")
	}

	if locator == nil {
		panic("driver locator is required")
	}

	if store == nil {
		panic("stop store is required")
	}

	return &stopService{Service: base, locator: locator, store: store}
}

type stopService struct {
	Service

	locator DriverLocator
	store   StopStore
}

func (s *stopService) Unwrap() Service { return s.Service }

func (s *stopService) ReachStop(ctx context.Context, tripID string, position int) (Trip, error) {
	tripID = strings.TrimSpace(tripID)
	if tripID == "" {
		return Trip{}, ErrTripIDRequired
	}

	current, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return Trip{}, err
	}

	if position < 1 || position > len(current.Stops) {
		return Trip{}, ErrStopNotFound
	}

	if current.Status != StatusInProgress || current.DriverID == "" {
		return Trip{}, ErrInvalidTransition
	}

	stop := current.Stops[position-1]
	if stop.ReachedAt != nil {
		return current, nil
	}

	here, err := s.locator.DriverLocation(ctx, current.DriverID)
	if errors.Is(err, ErrDriverLocationUnavailable) {
		return Trip{}, ErrArrivalPositionUnknown
	}

	if err != nil {
		return Trip{}, err
	}

	if distanceMeters(Coordinates{Latitude: here.Latitude, Longitude: here.Longitude}, stop.Coordinates) > arrivalRadiusMeters {
		return Trip{}, ErrTooFarFromStop
	}

	return s.store.MarkStopReached(ctx, tripID, position)
}
