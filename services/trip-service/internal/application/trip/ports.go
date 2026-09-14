package trip

import "context"

type CreateInput struct {
	ID      string
	RiderID string
	Pickup  Coordinates
	Dropoff Coordinates
}

// Repository is the persistence port for the trip aggregate. Every
// mutating method must be transactional with its outbox write, same
// pattern as rider-service and driver-service.
type Repository interface {
	Create(
		ctx context.Context,
		input CreateInput,
	) (Trip, error)

	FindByID(
		ctx context.Context,
		tripID string,
	) (Trip, error)

	// FindActiveByRiderID/FindActiveByDriverID return a trip whose status
	// is one of requested/accepted/in_progress — used to enforce "one
	// active trip at a time" per rider and per driver. Return
	// ErrTripNotFound when there is none; that's the expected, common
	// case, not an error condition for the caller.
	FindActiveByRiderID(
		ctx context.Context,
		riderID string,
	) (Trip, error)

	FindActiveByDriverID(
		ctx context.Context,
		driverID string,
	) (Trip, error)

	// Accept/Start/Complete/Cancel each load the current row, verify the
	// transition is legal via Status.CanTransitionTo, and persist the
	// new status plus its timestamp column and outbox event atomically.
	// They return ErrInvalidTransition (not a generic DB error) when the
	// trip's current status doesn't allow the requested move.
	Accept(
		ctx context.Context,
		tripID string,
		driverID string,
	) (Trip, error)

	Start(
		ctx context.Context,
		tripID string,
	) (Trip, error)

	Complete(
		ctx context.Context,
		tripID string,
	) (Trip, error)

	Cancel(
		ctx context.Context,
		tripID string,
		reason string,
	) (Trip, error)
}

type IDGenerator interface {
	NewID() string
}
