package zone

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) CreateZone(
	ctx context.Context,
	input CreateZoneInput,
) (Zone, error) {
	city := strings.TrimSpace(input.City)
	if city == "" {
		return Zone{}, ErrCityRequired
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Zone{}, ErrNameRequired
	}

	boundary, err := NewBoundary(input.Boundary)
	if err != nil {
		return Zone{}, err
	}

	created, err := s.repository.Create(ctx, CreateInput{
		ID:       s.idGenerator.NewID(),
		City:     city,
		Name:     name,
		Boundary: boundary,
	})
	if err != nil {
		return Zone{}, fmt.Errorf("create zone: %w", err)
	}

	return created, nil
}
