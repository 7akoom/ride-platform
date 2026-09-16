package zone

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) UpdateZone(
	ctx context.Context,
	input UpdateZoneInput,
) (Zone, error) {
	zoneID := strings.TrimSpace(input.ZoneID)
	if zoneID == "" {
		return Zone{}, ErrZoneIDRequired
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Zone{}, ErrNameRequired
	}

	boundary, err := NewBoundary(input.Boundary)
	if err != nil {
		return Zone{}, err
	}

	updated, err := s.repository.Update(ctx, UpdateInput{
		ZoneID:   zoneID,
		Name:     name,
		Boundary: boundary,
	})
	if err != nil {
		return Zone{}, fmt.Errorf("update zone: %w", err)
	}

	return updated, nil
}

func (s *service) SetZoneActive(
	ctx context.Context,
	zoneID string,
	active bool,
) (Zone, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return Zone{}, ErrZoneIDRequired
	}

	updated, err := s.repository.SetActive(ctx, zoneID, active)
	if err != nil {
		return Zone{}, fmt.Errorf("set zone active: %w", err)
	}

	return updated, nil
}
