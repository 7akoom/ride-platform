package location

import (
	"context"
	"fmt"
)

const defaultNearbyLimit = 20
const maxNearbyLimit = 100

func (s *service) FindNearby(
	ctx context.Context,
	input FindNearbyInput,
) ([]NearbyEntity, error) {
	if !input.EntityType.Valid() {
		return nil, ErrInvalidEntityType
	}

	coordinates, err := NewCoordinates(input.Latitude, input.Longitude)
	if err != nil {
		return nil, err
	}

	if input.RadiusMeters <= 0 {
		return nil, ErrInvalidRadius
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultNearbyLimit
	}
	if limit > maxNearbyLimit {
		limit = maxNearbyLimit
	}

	results, err := s.repository.FindNearby(
		ctx,
		NearbySearchInput{
			EntityType:   input.EntityType,
			Coordinates:  coordinates,
			RadiusMeters: input.RadiusMeters,
			Limit:        limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("find nearby entities: %w", err)
	}

	return results, nil
}
