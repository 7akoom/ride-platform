package trip

import (
	"context"
	"errors"
)

var (
	// ErrTripHasNoDriver: no driver has accepted the trip yet, so there is nobody to describe.
	ErrTripHasNoDriver = errors.New("this trip has no driver yet")

	// ErrDriverProfileUnavailable: the trip has a driver but their profile cannot be read.
	ErrDriverProfileUnavailable = errors.New("the driver's profile is not available")
)

// DriverSummary is what a rider may know about the driver of their trip: enough to
// recognise the person and the car. It has no phone number, no identity and nothing about
// the driver's account.
type DriverSummary struct {
	DisplayName   string
	VehicleMake   string
	VehicleModel  string
	VehicleColor  string
	PlateNumber   string
	VehicleClass  string
	RatingAverage float64
	RatingCount   int32
}

// DriverDirectory is the outbound port that reads a driver's profile from driver-service.
type DriverDirectory interface {
	DriverSummary(ctx context.Context, driverID string) (DriverSummary, error)
}

// WithDriverProfile decorates a Service with GetTripDriver, the rider's view of who is
// driving their trip. Like WithDriverTracking, it leaves the Service interface as it is:
// the transport layer reaches it through an optional interface (see As).
func WithDriverProfile(base Service, directory DriverDirectory) Service {
	if base == nil {
		panic("trip service is required")
	}

	if directory == nil {
		panic("driver directory is required")
	}

	return &driverProfileService{Service: base, directory: directory}
}

type driverProfileService struct {
	Service

	directory DriverDirectory
}

// Unwrap returns the Service the decorator wraps, so As can keep looking through it.
func (s *driverProfileService) Unwrap() Service { return s.Service }

// GetTripDriver describes the driver assigned to the trip. Who may ask (the rider of the
// trip, and nobody else) is decided before this is called; this only decides whether
// there is anything to show.
func (s *driverProfileService) GetTripDriver(ctx context.Context, tripID string) (DriverSummary, error) {
	found, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return DriverSummary{}, err
	}

	if found.DriverID == "" {
		return DriverSummary{}, ErrTripHasNoDriver
	}

	return s.directory.DriverSummary(ctx, found.DriverID)
}
