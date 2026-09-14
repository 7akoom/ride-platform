package location

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *service) UpdateLocation(
	ctx context.Context,
	input UpdateLocationInput,
) (time.Time, error) {
	if !input.EntityType.Valid() {
		return time.Time{}, ErrInvalidEntityType
	}

	entityID := strings.TrimSpace(input.EntityID)
	if entityID == "" {
		return time.Time{}, ErrEntityIDRequired
	}

	coordinates, err := NewCoordinates(input.Latitude, input.Longitude)
	if err != nil {
		return time.Time{}, err
	}

	updatedAt, err := s.repository.Update(
		ctx,
		UpdateInput{
			EntityType:  input.EntityType,
			EntityID:    entityID,
			Coordinates: coordinates,
			TTL:         DefaultTTL,
		},
	)
	if err != nil {
		return time.Time{}, fmt.Errorf("update location: %w", err)
	}

	return updatedAt, nil
}
