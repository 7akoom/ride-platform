package trip

import (
	"context"
	"errors"
	"time"
)

// DriverLocation is where a trip's driver last reported being.
type DriverLocation struct {
	Latitude  float64
	Longitude float64
	UpdatedAt time.Time
}

var (
	// ErrTripNotTrackable means the trip is not in a state where its driver's
	// position is shared with the rider: before a driver accepted, or after the
	// trip ended.
	ErrTripNotTrackable = errors.New("the driver's location is available only while a trip is accepted or in progress")

	// ErrDriverLocationUnavailable means the driver has not reported a position
	// recently (live positions expire after 30 seconds). It is expected and
	// transient: the rider app just keeps polling.
	ErrDriverLocationUnavailable = errors.New("the driver's location is not available right now")
)

// DriverLocator is the outbound port that looks up a driver's live position.
type DriverLocator interface {
	DriverLocation(ctx context.Context, driverID string) (DriverLocation, error)
}

// WithDriverTracking decorates a Service with GetDriverLocation, the rider's
// live view of the driver of a trip. Every other method is the base service's.
// The transport layer reaches it through a small optional interface, so the
// Service interface (and every fake of it) stays unchanged.
func WithDriverTracking(base Service, locator DriverLocator) Service {
	if base == nil {
		panic("trip service is required")
	}

	if locator == nil {
		panic("driver locator is required")
	}

	return &trackingService{Service: base, locator: locator}
}

type trackingService struct {
	Service

	locator DriverLocator
}

// GetDriverLocation returns the position of the driver assigned to the trip,
// but only while the trip is accepted or in progress. Who may ask is decided
// before this is called; this only decides whether there is anything to show.
func (s *trackingService) GetDriverLocation(ctx context.Context, tripID string) (DriverLocation, error) {
	found, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return DriverLocation{}, err
	}

	if found.Status != StatusAccepted && found.Status != StatusInProgress {
		return DriverLocation{}, ErrTripNotTrackable
	}

	if found.DriverID == "" {
		return DriverLocation{}, ErrTripNotTrackable
	}

	return s.locator.DriverLocation(ctx, found.DriverID)
}
