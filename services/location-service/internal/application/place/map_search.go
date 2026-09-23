package place

import (
	"context"
	"strings"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/localized"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

var _ maps.CuratedSearcher = (*Service)(nil)

// SearchCurated answers the map search with curated places, named in the
// first of the preferred languages that has a name.
func (s *Service) SearchCurated(
	ctx context.Context,
	query string,
	near *maps.Coordinates,
	limit int,
	languages []string,
) ([]maps.Place, error) {
	var point *Coordinates
	if near != nil {
		point = &Coordinates{Latitude: near.Latitude, Longitude: near.Longitude}
	}

	matches, err := s.Search(ctx, query, point, limit)
	if err != nil {
		return nil, err
	}

	places := make([]maps.Place, 0, len(matches))

	for _, match := range matches {
		places = append(places, ToMapPlace(match.Place, languages))
	}

	return places, nil
}

// ToMapPlace shows a curated place the way map results are shown.
func ToMapPlace(p Place, languages []string) maps.Place {
	name := localized.Pick(p.Name, p.Names, languages)
	cityName := localized.Pick(p.CityName, p.CityNames, languages)

	parts := []string{name}
	if p.Address != "" {
		parts = append(parts, p.Address)
	}

	parts = append(parts, cityName)

	return maps.Place{
		ID:             "curated/" + p.ID,
		Name:           name,
		DisplayName:    strings.Join(parts, ", "),
		Category:       string(p.Category),
		Type:           "curated",
		Coordinates:    maps.Coordinates{Latitude: p.Coordinates.Latitude, Longitude: p.Coordinates.Longitude},
		Address:        map[string]string{"city": cityName},
		CuratedPlaceID: p.ID,
	}
}
