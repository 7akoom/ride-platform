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
	walletClient   WalletClient
}

func NewService(
	tripClient TripClient,
	locationClient LocationClient,
	driverClient DriverClient,
	walletClient WalletClient,
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

	if walletClient == nil {
		panic("wallet client is required")
	}

	return &service{
		tripClient:     tripClient,
		locationClient: locationClient,
		driverClient:   driverClient,
		walletClient:   walletClient,
	}
}
