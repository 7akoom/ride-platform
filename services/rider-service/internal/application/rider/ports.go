package rider

import (
	"context"
	"time"
)

// CreateInput carries the data required to persist a new rider.
type CreateInput struct {
	ID          string
	IdentityID  string
	DisplayName string
}

// UpdateProfileInput carries the mutable profile fields for an update.
type UpdateProfileInput struct {
	RiderID     string
	DisplayName string
}

// Repository is the persistence port for the rider aggregate.
//
// Implementations must be transactional for Create: the rider row and the
// corresponding outbox event are written atomically so that a published
// "rider.created" event is never observed without a durable rider record,
// and vice versa.
type Repository interface {
	Create(
		ctx context.Context,
		input CreateInput,
	) (Rider, error)

	FindByID(
		ctx context.Context,
		riderID string,
	) (Rider, error)

	FindByIdentityID(
		ctx context.Context,
		identityID string,
	) (Rider, error)

	UpdateProfile(
		ctx context.Context,
		input UpdateProfileInput,
	) (Rider, error)
}

// IDGenerator produces new aggregate identifiers.
type IDGenerator interface {
	NewID() string
}

// Clock abstracts time for deterministic testing.
type Clock interface {
	Now() time.Time
}
