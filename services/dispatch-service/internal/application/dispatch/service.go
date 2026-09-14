package dispatch

import "context"

type Service interface {
	DispatchTrip(
		ctx context.Context,
		tripID string,
		searchRadiusMeters float64,
	) (Result, error)
}

type service struct {
	tripClient     TripClient
	locationClient LocationClient
	driverClient   DriverClient
}

func NewService(
	tripClient TripClient,
	locationClient LocationClient,
	driverClient DriverClient,
) Service {
	if tripClient == nil {
		panic("trip client is required")
	}

	if locationClient == nil {
		panic("location client is required")
	}

	if driverClient == nil {
		panic("driver client is required")
	}

	return &service{
		tripClient:     tripClient,
		locationClient: locationClient,
		driverClient:   driverClient,
	}
}
