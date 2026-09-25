package maps

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRouter struct {
	route Route
	err   error

	from, to Coordinates
	via      []Coordinates
	calls    int
}

func (f *fakeRouter) Route(_ context.Context, from Coordinates, to Coordinates, via ...Coordinates) (Route, error) {
	f.calls++
	f.from, f.to, f.via = from, to, via

	return f.route, f.err
}

type fakeGeocoder struct {
	places []Place
	place  Place
	err    error

	query     string
	near      *Coordinates
	limit     int
	languages string
	at        Coordinates
	calls     int
}

func (f *fakeGeocoder) Search(_ context.Context, query string, near *Coordinates, limit int, languages string) ([]Place, error) {
	f.calls++
	f.query, f.near, f.limit, f.languages = query, near, limit, languages

	return f.places, f.err
}

func (f *fakeGeocoder) Reverse(_ context.Context, at Coordinates, languages string) (Place, error) {
	f.calls++
	f.at, f.languages = at, languages

	return f.place, f.err
}

func point(lat, lng float64) *Coordinates { return &Coordinates{Latitude: lat, Longitude: lng} }

func newRig() (Service, *fakeRouter, *fakeGeocoder) {
	router := &fakeRouter{route: Route{DistanceMeters: 8693, DurationSeconds: 806, Polyline: "abc"}}
	geocoder := &fakeGeocoder{}

	return NewService(router, geocoder), router, geocoder
}

func TestARouteBetweenTwoValidPointsIsAskedOfTheRouter(t *testing.T) {
	service, router, _ := newRig()

	route, err := service.GetRoute(context.Background(), RouteInput{Origin: point(36.19, 44.01), Destination: point(36.23, 43.96)})
	if err != nil || route.DistanceMeters != 8693 || route.Polyline != "abc" {
		t.Fatalf("got %+v, %v", route, err)
	}

	if router.from != *point(36.19, 44.01) || router.to != *point(36.23, 43.96) {
		t.Errorf("the router was asked for %v to %v", router.from, router.to)
	}
}

func TestARouteNeedsTwoRealPointsAndNeverReachesTheRouterWithout(t *testing.T) {
	cases := map[string]struct {
		input RouteInput
		want  error
	}{
		"no origin":            {RouteInput{Destination: point(36, 44)}, ErrPointRequired},
		"no destination":       {RouteInput{Origin: point(36, 44)}, ErrPointRequired},
		"a latitude too high":  {RouteInput{Origin: point(91, 44), Destination: point(36, 44)}, ErrInvalidLatitude},
		"a latitude too low":   {RouteInput{Origin: point(36, 44), Destination: point(-90.5, 44)}, ErrInvalidLatitude},
		"a longitude too high": {RouteInput{Origin: point(36, 181), Destination: point(36, 44)}, ErrInvalidLongitude},
		"a longitude too low":  {RouteInput{Origin: point(36, 44), Destination: point(36, -180.1)}, ErrInvalidLongitude},
	}

	for name, tc := range cases {
		service, router, _ := newRig()

		if _, err := service.GetRoute(context.Background(), tc.input); !errors.Is(err, tc.want) {
			t.Errorf("%s: expected %v, got %v", name, tc.want, err)
		}

		if router.calls != 0 {
			t.Errorf("%s: the router was asked", name)
		}
	}
}

func TestARoutePassesThroughItsViaPointsInOrder(t *testing.T) {
	service, router, _ := newRig()

	if _, err := service.GetRoute(context.Background(), RouteInput{
		Origin: point(36.19, 44.01), Destination: point(36.23, 43.96),
		Via: []*Coordinates{point(36.2, 44.0), point(36.21, 43.98)},
	}); err != nil {
		t.Fatal(err)
	}

	if len(router.via) != 2 || router.via[0] != *point(36.2, 44.0) || router.via[1] != *point(36.21, 43.98) {
		t.Fatalf("via %v", router.via)
	}

	for name, via := range map[string][]*Coordinates{
		"six":       {point(36, 44), point(36, 44), point(36, 44), point(36, 44), point(36, 44), point(36, 44)},
		"a bad one": {point(95, 44)},
		"a nil one": {nil},
	} {
		service, router, _ := newRig()

		if _, err := service.GetRoute(context.Background(), RouteInput{Origin: point(36, 44), Destination: point(36.1, 44.1), Via: via}); err == nil || router.calls != 0 {
			t.Errorf("%s: %v, %d calls", name, err, router.calls)
		}
	}
}

func TestTheRoutersRefusalsPassThroughUnchanged(t *testing.T) {
	for _, refusal := range []error{ErrNoRoute, ErrNotNearRoad, ErrUnavailable} {
		service, router, _ := newRig()
		router.err = refusal

		if _, err := service.GetRoute(context.Background(), RouteInput{Origin: point(36, 44), Destination: point(36.1, 44.1)}); !errors.Is(err, refusal) {
			t.Errorf("expected %v, got %v", refusal, err)
		}
	}
}

func TestASearchIsAskedOfTheGeocoderWithTrimmedQueryLimitAndLanguages(t *testing.T) {
	service, _, geocoder := newRig()
	geocoder.places = []Place{{ID: "way/1", Name: "Erbil Citadel"}}

	places, err := service.SearchPlaces(context.Background(), SearchInput{Query: "  قلعة أربيل  ", Near: point(36.19, 44.01), Limit: 3, Language: "KU"})
	if err != nil || len(places) != 1 {
		t.Fatalf("got %+v, %v", places, err)
	}

	if geocoder.query != "قلعة أربيل" || geocoder.limit != 3 || geocoder.languages != "ku,ar,en" || geocoder.near == nil || *geocoder.near != *point(36.19, 44.01) {
		t.Errorf("unexpected question: %q %d %q %v", geocoder.query, geocoder.limit, geocoder.languages, geocoder.near)
	}
}

func TestSearchLimitsAndLanguages(t *testing.T) {
	limits := map[int]int{0: DefaultSearchLimit, 1: 1, MaxSearchLimit: MaxSearchLimit, MaxSearchLimit + 40: MaxSearchLimit}
	for asked, want := range limits {
		service, _, geocoder := newRig()

		if _, err := service.SearchPlaces(context.Background(), SearchInput{Query: "erbil", Limit: asked}); err != nil {
			t.Fatalf("limit %d: %v", asked, err)
		}

		if geocoder.limit != want {
			t.Errorf("limit %d: the geocoder was asked for %d, expected %d", asked, geocoder.limit, want)
		}
	}

	languages := map[string]string{"": "ar,ku,en", "ar": "ar,ku,en", "ku": "ku,ar,en", "en": "en,ar,ku", " EN ": "en,ar,ku"}
	for asked, want := range languages {
		service, _, geocoder := newRig()

		if _, err := service.SearchPlaces(context.Background(), SearchInput{Query: "erbil", Language: asked}); err != nil {
			t.Fatalf("language %q: %v", asked, err)
		}

		if geocoder.languages != want {
			t.Errorf("language %q: the geocoder was asked for %q, expected %q", asked, geocoder.languages, want)
		}
	}
}

func TestABadSearchNeverReachesTheGeocoder(t *testing.T) {
	cases := map[string]struct {
		input SearchInput
		want  error
	}{
		"an empty query":         {SearchInput{Query: ""}, ErrQueryTooShort},
		"a blank query":          {SearchInput{Query: "   "}, ErrQueryTooShort},
		"one character":          {SearchInput{Query: "أ"}, ErrQueryTooShort},
		"a very long query":      {SearchInput{Query: strings.Repeat("x", 201)}, ErrQueryTooLong},
		"a negative limit":       {SearchInput{Query: "erbil", Limit: -1}, ErrInvalidLimit},
		"a language we lack":     {SearchInput{Query: "erbil", Language: "fr"}, ErrInvalidLanguage},
		"a bad point to rank by": {SearchInput{Query: "erbil", Near: point(95, 44)}, ErrInvalidLatitude},
	}

	for name, tc := range cases {
		service, _, geocoder := newRig()

		if _, err := service.SearchPlaces(context.Background(), tc.input); !errors.Is(err, tc.want) {
			t.Errorf("%s: expected %v, got %v", name, tc.want, err)
		}

		if geocoder.calls != 0 {
			t.Errorf("%s: the geocoder was asked", name)
		}
	}
}

func TestTwoCharactersAreEnoughAndCountedAsCharactersNotBytes(t *testing.T) {
	service, _, geocoder := newRig()

	// Two Arabic letters are four bytes but two characters.
	if _, err := service.SearchPlaces(context.Background(), SearchInput{Query: "دهوك"[:4]}); err != nil {
		t.Errorf("two characters must be accepted: %v", err)
	}

	if geocoder.calls != 1 {
		t.Errorf("the geocoder must be asked once, was %d", geocoder.calls)
	}

	if _, err := service.SearchPlaces(context.Background(), SearchInput{Query: strings.Repeat("أ", 200)}); err != nil {
		t.Errorf("200 characters (400 bytes) must be accepted: %v", err)
	}
}

func TestNeverMoreThanAskedFor(t *testing.T) {
	service, _, geocoder := newRig()
	geocoder.places = []Place{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}}

	places, err := service.SearchPlaces(context.Background(), SearchInput{Query: "erbil", Limit: 2})
	if err != nil || len(places) != 2 {
		t.Errorf("got %d places, %v", len(places), err)
	}
}

func TestAReverseLookupIsAskedOfTheGeocoder(t *testing.T) {
	service, _, geocoder := newRig()
	geocoder.place = Place{ID: "way/9", DisplayName: "Ankawa, Erbil"}

	place, err := service.ReverseGeocode(context.Background(), ReverseInput{Coordinates: point(36.23, 43.97), Language: "en"})
	if err != nil || place.ID != "way/9" {
		t.Fatalf("got %+v, %v", place, err)
	}

	if geocoder.at != *point(36.23, 43.97) || geocoder.languages != "en,ar,ku" {
		t.Errorf("unexpected question: %v %q", geocoder.at, geocoder.languages)
	}
}

func TestABadReverseLookupNeverReachesTheGeocoder(t *testing.T) {
	cases := map[string]struct {
		input ReverseInput
		want  error
	}{
		"no point":           {ReverseInput{}, ErrPointRequired},
		"a bad latitude":     {ReverseInput{Coordinates: point(-91, 0)}, ErrInvalidLatitude},
		"a bad longitude":    {ReverseInput{Coordinates: point(0, 200)}, ErrInvalidLongitude},
		"a language we lack": {ReverseInput{Coordinates: point(36, 44), Language: "tr"}, ErrInvalidLanguage},
	}

	for name, tc := range cases {
		service, _, geocoder := newRig()

		if _, err := service.ReverseGeocode(context.Background(), tc.input); !errors.Is(err, tc.want) {
			t.Errorf("%s: expected %v, got %v", name, tc.want, err)
		}

		if geocoder.calls != 0 {
			t.Errorf("%s: the geocoder was asked", name)
		}
	}
}

func TestTheGeocodersRefusalsPassThroughUnchanged(t *testing.T) {
	for _, refusal := range []error{ErrPlaceNotFound, ErrUnavailable} {
		service, _, geocoder := newRig()
		geocoder.err = refusal

		if _, err := service.ReverseGeocode(context.Background(), ReverseInput{Coordinates: point(36, 44)}); !errors.Is(err, refusal) {
			t.Errorf("reverse: expected %v, got %v", refusal, err)
		}

		if _, err := service.SearchPlaces(context.Background(), SearchInput{Query: "erbil"}); !errors.Is(err, refusal) {
			t.Errorf("search: expected %v, got %v", refusal, err)
		}
	}
}

func TestTheServiceRequiresBothParts(t *testing.T) {
	for name, build := range map[string]func(){
		"no router":   func() { NewService(nil, &fakeGeocoder{}) },
		"no geocoder": func() { NewService(&fakeRouter{}, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected a panic", name)
				}
			}()

			build()
		}()
	}
}
