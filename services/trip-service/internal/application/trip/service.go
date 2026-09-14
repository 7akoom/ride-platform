package trip

import "context"

type RequestTripInput struct {
	RiderID    string
	PickupLat  float64
	PickupLng  float64
	DropoffLat float64
	DropoffLng float64
}

type Service interface {
	RequestTrip(
		ctx context.Context,
		input RequestTripInput,
	) (Trip, error)

	AcceptTrip(
		ctx context.Context,
		tripID string,
		driverID string,
	) (Trip, error)

	StartTrip(
		ctx context.Context,
		tripID string,
	) (Trip, error)

	CompleteTrip(
		ctx context.Context,
		tripID string,
	) (Trip, error)

	CancelTrip(
		ctx context.Context,
		tripID string,
		reason string,
	) (Trip, error)

	GetTrip(
		ctx context.Context,
		tripID string,
	) (Trip, error)
}

type service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(
	repository Repository,
	idGenerator IDGenerator,
) Service {
	if repository == nil {
		panic("trip repository is required")
	}

	if idGenerator == nil {
		panic("trip id generator is required")
	}

	return &service{
		repository:  repository,
		idGenerator: idGenerator,
	}
}
