package trip

import (
	"context"
	"time"
)

type RequestTripInput struct {
	RiderID       string
	PickupLat     float64
	PickupLng     float64
	DropoffLat    float64
	DropoffLng    float64
	VehicleClass  string
	PaymentMethod string

	// As the rider picked them; at most 300 characters each.
	PickupAddress  string
	DropoffAddress string

	// A saved address of the rider's. WithSavedAddresses turns them into the
	// fields above and below; the base service refuses them.
	PickupSavedAddressID  string
	DropoffSavedAddressID string

	// Copied from the saved pickup address.
	PickupDetails      string
	PickupNote         string
	PickupPhotoMediaID string
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

	// TriggerSOS is a safety action, not a lifecycle transition — see
	// Repository.TriggerSOS for why it doesn't touch trip status.
	TriggerSOS(
		ctx context.Context,
		tripID string,
		triggeredBy SosTriggeredBy,
		latitude, longitude float64,
	) (alertID string, triggeredAt time.Time, err error)

	RecordWaypoint(
		ctx context.Context,
		tripID string,
		latitude, longitude float64,
	) error

	GetTripPath(
		ctx context.Context,
		tripID string,
	) ([]Waypoint, error)

	// RateTrip records one side's rating of the other after a completed trip.
	RateTrip(ctx context.Context, input RateTripInput) (Rating, error)
}

type service struct {
	repository  Repository
	idGenerator IDGenerator
	zoneChecker ZoneChecker
}

func NewService(
	repository Repository,
	idGenerator IDGenerator,
	zoneChecker ZoneChecker,
) Service {
	if repository == nil {
		panic("trip repository is required")
	}

	if idGenerator == nil {
		panic("trip id generator is required")
	}

	if zoneChecker == nil {
		panic("trip zone checker is required")
	}

	return &service{
		repository:  repository,
		idGenerator: idGenerator,
		zoneChecker: zoneChecker,
	}
}
