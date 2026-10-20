package trip

import (
	"context"
	"fmt"
	"strings"
)

// TripCounter counts completed trips, for a rider or a driver, all time.
type TripCounter interface {
	CountRiderCompletedTrips(ctx context.Context, riderID string) (int, error)
	CountDriverCompletedTrips(ctx context.Context, driverID string) (int, error)
}

// WithTripCounts decorates a Service with RiderCompletedTrips (the rider's account
// header) and DriverCompletedTripsOfTrip (the driver card on the rider's trip). Who
// may ask is decided before these are called.
func WithTripCounts(base Service, counter TripCounter) Service {
	if base == nil {
		panic("trip service is required")
	}

	if counter == nil {
		panic("trip counter is required")
	}

	return &tripCountService{Service: base, counter: counter}
}

type tripCountService struct {
	Service

	counter TripCounter
}

func (s *tripCountService) Unwrap() Service { return s.Service }

// RiderCompletedTrips is how many trips the rider has completed.
func (s *tripCountService) RiderCompletedTrips(ctx context.Context, riderID string) (int, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return 0, ErrRiderIDRequired
	}

	count, err := s.counter.CountRiderCompletedTrips(ctx, riderID)
	if err != nil {
		return 0, fmt.Errorf("count rider trips: %w", err)
	}

	return count, nil
}

// DriverCompletedTripsOfTrip is how many trips the driver of tripID has completed;
// ErrTripHasNoDriver before one accepted it.
func (s *tripCountService) DriverCompletedTripsOfTrip(ctx context.Context, tripID string) (int, error) {
	found, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return 0, err
	}

	if found.DriverID == "" {
		return 0, ErrTripHasNoDriver
	}

	count, err := s.counter.CountDriverCompletedTrips(ctx, found.DriverID)
	if err != nil {
		return 0, fmt.Errorf("count driver trips: %w", err)
	}

	return count, nil
}
