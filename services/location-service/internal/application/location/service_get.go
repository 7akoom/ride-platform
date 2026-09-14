package location

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) GetLocation(
	ctx context.Context,
	entityType EntityType,
	entityID string,
) (Location, error) {
	if !entityType.Valid() {
		return Location{}, ErrInvalidEntityType
	}

	trimmedID := strings.TrimSpace(entityID)
	if trimmedID == "" {
		return Location{}, ErrEntityIDRequired
	}

	found, err := s.repository.Get(ctx, entityType, trimmedID)
	if err != nil {
		return Location{}, fmt.Errorf("get location: %w", err)
	}

	return found, nil
}
