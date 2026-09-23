package trip

import (
	"context"
	"time"
)

type CreateInput struct {
	ID            string
	RiderID       string
	Pickup        Coordinates
	Dropoff       Coordinates
	VehicleClass  string
	PaymentMethod string

	PickupAddress      string
	DropoffAddress     string
	PickupDetails      string
	PickupNote         string
	PickupPhotoMediaID string

	// The quote the trip was requested with and its price; empty without one.
	QuoteID      string
	QuotedFare   string
	CurrencyCode string
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

	// TriggerSOS records a safety alert against a trip and returns its
	// generated ID and timestamp. Unlike Accept/Start/Complete/Cancel
	// it is NOT a status transition — an SOS alert doesn't change what
	// state the trip is in, it's a side record alongside it, so it
	// requires only that the trip exist, not any particular status.
	TriggerSOS(
		ctx context.Context,
		tripID string,
		triggeredBy SosTriggeredBy,
		location Coordinates,
	) (alertID string, triggeredAt time.Time, err error)

	// RecordWaypointIfDue appends one path point for tripID, unless the
	// most recently recorded point for that trip is younger than
	// minInterval — in which case it does nothing and returns nil, not
	// an error. The throttling lives here (at the write) rather than
	// on the caller, so every caller gets the same behavior for free.
	RecordWaypointIfDue(
		ctx context.Context,
		tripID string,
		location Coordinates,
		recordedAt time.Time,
		minInterval time.Duration,
	) error

	// ListWaypoints returns every recorded point for tripID, oldest
	// first.
	ListWaypoints(
		ctx context.Context,
		tripID string,
	) ([]Waypoint, error)

	// CreateRating stores a rating and its trip.rated event in one transaction.
	// It returns ErrAlreadyRated when that side already rated the trip.
	CreateRating(ctx context.Context, input CreateRatingInput) (Rating, error)
}

type IDGenerator interface {
	NewID() string
}

// ZoneChecker is trip-service's first outbound peer dependency — it
// calls location-service to enforce that a trip can only be requested
// with a pickup point inside an active service zone.
type ZoneChecker interface {
	CheckServiceZone(
		ctx context.Context,
		latitude, longitude float64,
	) (served bool, err error)
}
