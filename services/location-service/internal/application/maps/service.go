package maps

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	DefaultSearchLimit = 5
	MaxSearchLimit     = 10

	minQueryLength = 2
	maxQueryLength = 200
)

type Service interface {
	GetRoute(ctx context.Context, input RouteInput) (Route, error)
	SearchPlaces(ctx context.Context, input SearchInput) ([]Place, error)
	ReverseGeocode(ctx context.Context, input ReverseInput) (Place, error)
}

type service struct {
	router   Router
	geocoder Geocoder
	curated  CuratedSearcher
	logger   *slog.Logger
}

// dedupeMeters: a map result this close to a curated place is the same place.
const dedupeMeters = 75

func NewService(router Router, geocoder Geocoder) Service {
	if router == nil {
		panic("router is required")
	}

	if geocoder == nil {
		panic("geocoder is required")
	}

	return &service{router: router, geocoder: geocoder}
}

// NewServiceWithCurated also searches curated places, which come before map
// results. If one of the two sources fails, the other's results are still
// returned (the failure is logged); only when both fail is the search an error.
func NewServiceWithCurated(router Router, geocoder Geocoder, curated CuratedSearcher, logger *slog.Logger) Service {
	if curated == nil || logger == nil {
		panic("curated searcher and logger are required")
	}

	s := NewService(router, geocoder).(*service)
	s.curated = curated
	s.logger = logger

	return s
}

func (s *service) GetRoute(ctx context.Context, input RouteInput) (Route, error) {
	if input.Origin == nil || input.Destination == nil {
		return Route{}, ErrPointRequired
	}

	if err := input.Origin.Validate(); err != nil {
		return Route{}, err
	}

	if err := input.Destination.Validate(); err != nil {
		return Route{}, err
	}

	return s.router.Route(ctx, *input.Origin, *input.Destination)
}

func (s *service) SearchPlaces(ctx context.Context, input SearchInput) ([]Place, error) {
	query := strings.TrimSpace(input.Query)

	switch length := utf8.RuneCountInString(query); {
	case length < minQueryLength:
		return nil, ErrQueryTooShort
	case length > maxQueryLength:
		return nil, ErrQueryTooLong
	}

	limit, err := searchLimit(input.Limit)
	if err != nil {
		return nil, err
	}

	languages, err := acceptLanguage(input.Language)
	if err != nil {
		return nil, err
	}

	if input.Near != nil {
		if err := input.Near.Validate(); err != nil {
			return nil, err
		}
	}

	var curated []Place

	if s.curated != nil {
		found, err := s.curated.SearchCurated(ctx, query, input.Near, limit, strings.Split(languages, ","))
		if err != nil {
			s.logger.Error("curated place search failed", "error", err)
		}

		curated = found
	}

	places, err := s.geocoder.Search(ctx, query, input.Near, limit, languages)
	if err != nil {
		if len(curated) == 0 {
			return nil, err
		}

		s.logger.Warn("map place search failed; answering with curated places only", "error", err)
		places = nil
	}

	merged := make([]Place, 0, limit)
	merged = append(merged, curated...)

	for _, place := range places {
		if !nearAny(place.Coordinates, curated, dedupeMeters) {
			merged = append(merged, place)
		}
	}

	// However many the sources return, never more than asked for.
	if len(merged) > limit {
		merged = merged[:limit]
	}

	return merged, nil
}

func nearAny(point Coordinates, places []Place, meters float64) bool {
	for _, p := range places {
		if DistanceMeters(point, p.Coordinates) <= meters {
			return true
		}
	}

	return false
}

// DistanceMeters is the great-circle distance between two points.
func DistanceMeters(a, b Coordinates) float64 {
	const earthRadius = 6371000.0

	lat1, lat2 := a.Latitude*math.Pi/180, b.Latitude*math.Pi/180
	dLat := lat2 - lat1
	dLng := (b.Longitude - a.Longitude) * math.Pi / 180

	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)

	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}

func (s *service) ReverseGeocode(ctx context.Context, input ReverseInput) (Place, error) {
	if input.Coordinates == nil {
		return Place{}, ErrPointRequired
	}

	if err := input.Coordinates.Validate(); err != nil {
		return Place{}, err
	}

	languages, err := acceptLanguage(input.Language)
	if err != nil {
		return Place{}, err
	}

	return s.geocoder.Reverse(ctx, *input.Coordinates, languages)
}

func searchLimit(requested int) (int, error) {
	switch {
	case requested < 0:
		return 0, ErrInvalidLimit
	case requested == 0:
		return DefaultSearchLimit, nil
	case requested > MaxSearchLimit:
		return MaxSearchLimit, nil
	default:
		return requested, nil
	}
}

// acceptLanguage turns the app's language into the ordered preference the geocoder
// uses: the language asked for first, then the others, so a place with no name in
// Kurdish still shows up with its Arabic or English name.
func acceptLanguage(language string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "ar":
		return "ar,ku,en", nil
	case "ku":
		return "ku,ar,en", nil
	case "en":
		return "en,ar,ku", nil
	default:
		return "", ErrInvalidLanguage
	}
}
