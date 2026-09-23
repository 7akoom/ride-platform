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
	cityID := strings.ToLower(strings.TrimSpace(input.CityID))
	switch {
	case cityID == "":
		return Zone{}, ErrCityRequired
	case !uuidShape.MatchString(cityID):
		return Zone{}, ErrCityNotFound
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
		CityID:   cityID,
		Name:     name,
		Boundary: boundary,
	})
	if err != nil {
		return Zone{}, fmt.Errorf("create zone: %w", err)
	}

	return created, nil
}
