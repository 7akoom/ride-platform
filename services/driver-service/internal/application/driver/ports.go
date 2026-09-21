package driver

import (
	"context"
	"time"
)

type CreateInput struct {
	ID          string
	IdentityID  string
	DisplayName string
	Vehicle     Vehicle
}

type UpdateProfileInput struct {
	DriverID    string
	DisplayName string
	Vehicle     Vehicle
}

type UpdateAvailabilityInput struct {
	DriverID           string
	AvailabilityStatus AvailabilityStatus
}

// UpdateStatusInput moves a driver to To, but only if its current status is
// one of AllowedFrom. The check and the write are one statement, so two
// operators acting at once cannot both win.
type UpdateStatusInput struct {
	DriverID    string
	To          Status
	AllowedFrom []Status
}

// Repository is the persistence port for the driver aggregate. Create must
// be transactional with the outbox write, same as rider-service.
type Repository interface {
	Create(
		ctx context.Context,
		input CreateInput,
	) (Driver, error)

	FindByID(
		ctx context.Context,
		driverID string,
	) (Driver, error)

	FindByIdentityID(
		ctx context.Context,
		identityID string,
	) (Driver, error)

	UpdateProfile(
		ctx context.Context,
		input UpdateProfileInput,
	) (Driver, error)

	UpdateAvailability(
		ctx context.Context,
		input UpdateAvailabilityInput,
	) (Driver, error)

	// UpdateStatus returns ErrDriverNotFound when the driver does not exist and
	// ErrInvalidStatusTransition when it exists but is not in one of AllowedFrom.
	// A driver already in To is returned unchanged with no error, so a retried
	// approval is harmless.
	UpdateStatus(
		ctx context.Context,
		input UpdateStatusInput,
	) (Driver, error)
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}
