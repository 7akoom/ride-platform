package maps

import (
	"context"
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

	places, err := s.geocoder.Search(ctx, query, input.Near, limit, languages)
	if err != nil {
		return nil, err
	}

	// However many the geocoder returns, never more than asked for.
	if len(places) > limit {
		places = places[:limit]
	}

	return places, nil
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
