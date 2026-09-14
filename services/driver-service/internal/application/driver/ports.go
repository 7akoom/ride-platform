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
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}
