package location

import (
	"context"
	"time"
)

// DefaultTTL is how long a location stays valid after its last update
// before it's treated as stale (entity went offline). 30s matches the
// industry-standard driver ping interval (2-5s) with generous slack.
const DefaultTTL = 30 * time.Second

type UpdateLocationInput struct {
	EntityType EntityType
	EntityID   string
	Latitude   float64
	Longitude  float64
}

type FindNearbyInput struct {
	EntityType   EntityType
	Latitude     float64
	Longitude    float64
	RadiusMeters float64
	Limit        int
}

type Service interface {
	UpdateLocation(
		ctx context.Context,
		input UpdateLocationInput,
	) (time.Time, error)

	GetLocation(
		ctx context.Context,
		entityType EntityType,
		entityID string,
	) (Location, error)

	FindNearby(
		ctx context.Context,
		input FindNearbyInput,
	) ([]NearbyEntity, error)
}

type service struct {
	repository Repository
}

func NewService(repository Repository) Service {
	if repository == nil {
		panic("location repository is required")
	}

	return &service{repository: repository}
}
