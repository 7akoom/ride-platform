package location

import (
	"context"
	"time"
)

type UpdateInput struct {
	EntityType  EntityType
	EntityID    string
	Coordinates Coordinates
	TTL         time.Duration
}

type NearbySearchInput struct {
	EntityType   EntityType
	Coordinates  Coordinates
	RadiusMeters float64
	Limit        int
}

// Repository is the persistence port for locations. Implementations are
// expected to be backed by an in-memory store with per-entry expiry
// (Valkey), never a durable database — see the Location type's comment
// for why.
type Repository interface {
	Update(
		ctx context.Context,
		input UpdateInput,
	) (time.Time, error)

	Get(
		ctx context.Context,
		entityType EntityType,
		entityID string,
	) (Location, error)

	FindNearby(
		ctx context.Context,
		input NearbySearchInput,
	) ([]NearbyEntity, error)
}

type Clock interface {
	Now() time.Time
}
