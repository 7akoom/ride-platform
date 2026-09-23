package zone

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) GetZone(
	ctx context.Context,
	zoneID string,
) (Zone, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return Zone{}, ErrZoneIDRequired
	}

	found, err := s.repository.Get(ctx, zoneID)
	if err != nil {
		return Zone{}, fmt.Errorf("get zone: %w", err)
	}

	return found, nil
}

func (s *service) ListZones(
	ctx context.Context,
	cityID string,
) ([]Zone, error) {
	cityID = strings.ToLower(strings.TrimSpace(cityID))
	if cityID != "" && !uuidShape.MatchString(cityID) {
		return []Zone{}, nil
	}

	results, err := s.repository.List(ctx, cityID)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}

	return results, nil
}

func (s *service) CheckServiceZone(
	ctx context.Context,
	latitude, longitude float64,
) (CheckServiceZoneResult, error) {
	point, err := NewCoordinates(latitude, longitude)
	if err != nil {
		return CheckServiceZoneResult{}, err
	}

	found, ok, err := s.repository.FindContaining(ctx, point)
	if err != nil {
		return CheckServiceZoneResult{}, fmt.Errorf("check service zone: %w", err)
	}

	if !ok {
		return CheckServiceZoneResult{Served: false}, nil
	}

	return CheckServiceZoneResult{
		Served:   true,
		ZoneID:   found.ID,
		CityID:   found.CityID,
		City:     found.City,
		TimeZone: found.TimeZone,
	}, nil
}
