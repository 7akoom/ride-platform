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
	TravelTimes(ctx context.Context, input TravelTimesInput) ([]TravelTime, error)
	SearchPlaces(ctx context.Context, input SearchInput) ([]Place, error)
	ReverseGeocode(ctx context.Context, input ReverseInput) (Place, error)
}

type service struct {
	router   Router
	geocoder Geocoder
	curated  CuratedSearcher
	imported ImportedSearcher
	logger   *slog.Logger
}

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

// NewServiceWithSources also searches imported places, which come after
// curated places and before the map's own results. As with curated places, a
// source that fails is logged and the others still answer.
func NewServiceWithSources(
	router Router,
	geocoder Geocoder,
	curated CuratedSearcher,
	imported ImportedSearcher,
	logger *slog.Logger,
) Service {
	if imported == nil {
		panic("imported searcher is required")
	}

	s := NewServiceWithCurated(router, geocoder, curated, logger).(*service)
	s.imported = imported

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

	if len(input.Via) > MaxVia {
		return Route{}, ErrTooManyVia
	}

	via := make([]Coordinates, 0, len(input.Via))

	for _, point := range input.Via {
		if point == nil {
			return Route{}, ErrPointRequired
		}

		if err := point.Validate(); err != nil {
			return Route{}, err
		}

		via = append(via, *point)
	}

	return s.router.Route(ctx, *input.Origin, *input.Destination, via...)
}

// TravelTimes is how long by road from each origin to the destination.
func (s *service) TravelTimes(ctx context.Context, input TravelTimesInput) ([]TravelTime, error) {
	if input.Destination == nil {
		return nil, ErrPointRequired
	}

	if err := input.Destination.Validate(); err != nil {
		return nil, err
	}

	switch {
	case len(input.Origins) == 0:
		return nil, ErrOriginsRequired
	case len(input.Origins) > MaxTravelOrigins:
		return nil, ErrTooManyOrigins
	}

	origins := make([]Coordinates, 0, len(input.Origins))

	for _, origin := range input.Origins {
		if origin == nil {
			return nil, ErrPointRequired
		}

		if err := origin.Validate(); err != nil {
			return nil, err
		}

		origins = append(origins, *origin)
	}

	times, err := s.router.TravelTimes(ctx, origins, *input.Destination)
	if err != nil {
		return nil, err
	}

	if len(times) != len(origins) {
		return nil, ErrUnavailable
	}

	return times, nil
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

	curated := s.searchCurated(ctx, query, input.Near, limit, languages)
	imported := s.searchImported(ctx, query, input.Near, limit)

	places, err := s.geocoder.Search(ctx, query, input.Near, limit, languages)
	if err != nil {
		if len(curated) == 0 && len(imported) == 0 {
			return nil, err
		}

		s.logger.Warn("map place search failed; answering with the other sources", "error", err)
		places = nil
	}

	return mergeResults(curated, imported, places, limit), nil
}

func (s *service) searchCurated(ctx context.Context, query string, near *Coordinates, limit int, languages string) []Place {
	if s.curated == nil {
		return nil
	}

	found, err := s.curated.SearchCurated(ctx, query, near, limit, strings.Split(languages, ","))
	if err != nil {
		s.logger.Error("curated place search failed", "error", err)
	}

	return found
}

func (s *service) searchImported(ctx context.Context, query string, near *Coordinates, limit int) []Place {
	if s.imported == nil {
		return nil
	}

	found, err := s.imported.SearchImported(ctx, query, near, limit)
	if err != nil {
		s.logger.Error("imported place search failed", "error", err)
	}

	return found
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
