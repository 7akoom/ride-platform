package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mapsFake struct {
	route  maps.Route
	places []maps.Place
	place  maps.Place
	err    error

	routeInput   maps.RouteInput
	searchInput  maps.SearchInput
	reverseInput maps.ReverseInput
}

func (f *mapsFake) GetRoute(_ context.Context, input maps.RouteInput) (maps.Route, error) {
	f.routeInput = input

	return f.route, f.err
}

func (f *mapsFake) SearchPlaces(_ context.Context, input maps.SearchInput) ([]maps.Place, error) {
	f.searchInput = input

	return f.places, f.err
}

func (f *mapsFake) ReverseGeocode(_ context.Context, input maps.ReverseInput) (maps.Place, error) {
	f.reverseInput = input

	return f.place, f.err
}

// The handler's other services are not used by the map RPCs; an embedded nil interface
// satisfies the constructor's checks without knowing their methods.
type mapsTestLocationService struct{ location.Service }

type mapsTestZoneService struct{ zone.Service }

func newMapsHandler(t *testing.T, service mapsService) *LocationHandler {
	t.Helper()

	handler := NewLocationHandler(mapsTestLocationService{}, mapsTestZoneService{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if service != nil {
		handler.WithMaps(service)
	}

	return handler
}

func coords(lat, lng float64) *locationv1.Coordinates {
	return &locationv1.Coordinates{Latitude: lat, Longitude: lng}
}

func TestGetRouteReturnsTheDistanceTheTimeAndTheLine(t *testing.T) {
	fake := &mapsFake{route: maps.Route{DistanceMeters: 8693.4, DurationSeconds: 806.1, Polyline: "_p~iF"}}

	response, err := newMapsHandler(t, fake).GetRoute(context.Background(), &locationv1.GetRouteRequest{Origin: coords(36.19, 44.01), Destination: coords(36.23, 43.96)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetDistanceMeters() != 8693.4 || response.GetDurationSeconds() != 806.1 || response.GetPolyline() != "_p~iF" {
		t.Errorf("unexpected response: %+v", response)
	}

	if fake.routeInput.Origin == nil || *fake.routeInput.Origin != (maps.Coordinates{Latitude: 36.19, Longitude: 44.01}) ||
		fake.routeInput.Destination == nil || *fake.routeInput.Destination != (maps.Coordinates{Latitude: 36.23, Longitude: 43.96}) {
		t.Errorf("unexpected input: %+v", fake.routeInput)
	}
}

func TestAMissingPointStaysMissingInsteadOfBecomingZeroZero(t *testing.T) {
	fake := &mapsFake{}

	_, _ = newMapsHandler(t, fake).GetRoute(context.Background(), &locationv1.GetRouteRequest{Origin: coords(36.19, 44.01)})

	if fake.routeInput.Destination != nil {
		t.Errorf("a missing destination must reach the service as missing, got %+v", fake.routeInput.Destination)
	}

	_, _ = newMapsHandler(t, fake).SearchPlaces(context.Background(), &locationv1.SearchPlacesRequest{Query: "erbil"})

	if fake.searchInput.Near != nil {
		t.Errorf("no point to rank by must reach the service as none, got %+v", fake.searchInput.Near)
	}
}

func TestSearchPlacesPassesTheRequestThroughAndConvertsThePlaces(t *testing.T) {
	fake := &mapsFake{places: []maps.Place{{
		ID: "way/1", Name: "Erbil Citadel", DisplayName: "Erbil Citadel, Erbil, Iraq", Category: "tourism", Type: "attraction",
		Coordinates: maps.Coordinates{Latitude: 36.1911, Longitude: 44.0092}, Address: map[string]string{"city": "Erbil"},
	}}}

	response, err := newMapsHandler(t, fake).SearchPlaces(context.Background(), &locationv1.SearchPlacesRequest{Query: "citadel", Near: coords(36.2, 44.0), Limit: 3, Language: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(response.GetPlaces()) != 1 {
		t.Fatalf("expected one place: %+v", response)
	}

	place := response.GetPlaces()[0]
	if place.GetId() != "way/1" || place.GetName() != "Erbil Citadel" || place.GetDisplayName() != "Erbil Citadel, Erbil, Iraq" ||
		place.GetCategory() != "tourism" || place.GetType() != "attraction" || place.GetCoordinates().GetLatitude() != 36.1911 ||
		place.GetCoordinates().GetLongitude() != 44.0092 || place.GetAddress()["city"] != "Erbil" {
		t.Errorf("unexpected place: %+v", place)
	}

	in := fake.searchInput
	if in.Query != "citadel" || in.Limit != 3 || in.Language != "en" || in.Near == nil || *in.Near != (maps.Coordinates{Latitude: 36.2, Longitude: 44.0}) {
		t.Errorf("unexpected input: %+v", in)
	}
}

func TestNothingFoundIsAnEmptyListNotNull(t *testing.T) {
	response, err := newMapsHandler(t, &mapsFake{}).SearchPlaces(context.Background(), &locationv1.SearchPlacesRequest{Query: "zzzzz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetPlaces() == nil || len(response.GetPlaces()) != 0 {
		t.Errorf("expected an empty, non-nil list: %#v", response.GetPlaces())
	}
}

func TestReverseGeocodeNamesThePoint(t *testing.T) {
	fake := &mapsFake{place: maps.Place{ID: "way/9", DisplayName: "Ankawa, Erbil", Coordinates: maps.Coordinates{Latitude: 36.2286, Longitude: 43.975}}}

	response, err := newMapsHandler(t, fake).ReverseGeocode(context.Background(), &locationv1.ReverseGeocodeRequest{Coordinates: coords(36.2286, 43.975), Language: "ku"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetPlace().GetId() != "way/9" || response.GetPlace().GetDisplayName() != "Ankawa, Erbil" {
		t.Errorf("unexpected place: %+v", response.GetPlace())
	}

	if fake.reverseInput.Coordinates == nil || fake.reverseInput.Language != "ku" {
		t.Errorf("unexpected input: %+v", fake.reverseInput)
	}
}

func TestEachRefusalHasItsOwnCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"no point", maps.ErrPointRequired, codes.InvalidArgument},
		{"a bad latitude", maps.ErrInvalidLatitude, codes.InvalidArgument},
		{"a bad longitude", maps.ErrInvalidLongitude, codes.InvalidArgument},
		{"a short query", maps.ErrQueryTooShort, codes.InvalidArgument},
		{"a long query", maps.ErrQueryTooLong, codes.InvalidArgument},
		{"a bad limit", maps.ErrInvalidLimit, codes.InvalidArgument},
		{"a bad language", maps.ErrInvalidLanguage, codes.InvalidArgument},
		{"no route", maps.ErrNoRoute, codes.NotFound},
		{"no place", maps.ErrPlaceNotFound, codes.NotFound},
		{"a point off the roads", maps.ErrNotNearRoad, codes.FailedPrecondition},
		{"a map server that is down", maps.ErrUnavailable, codes.Unavailable},
		{"wrapped", fmt.Errorf("%w: call OSRM: connection refused", maps.ErrUnavailable), codes.Unavailable},
		{"anything else", errors.New("boom"), codes.Internal},
	}

	for _, tc := range cases {
		handler := newMapsHandler(t, &mapsFake{err: tc.err})
		ctx := context.Background()

		_, routeErr := handler.GetRoute(ctx, &locationv1.GetRouteRequest{Origin: coords(1, 1), Destination: coords(2, 2)})
		_, searchErr := handler.SearchPlaces(ctx, &locationv1.SearchPlacesRequest{Query: "erbil"})
		_, reverseErr := handler.ReverseGeocode(ctx, &locationv1.ReverseGeocodeRequest{Coordinates: coords(1, 1)})

		for rpc, err := range map[string]error{"GetRoute": routeErr, "SearchPlaces": searchErr, "ReverseGeocode": reverseErr} {
			if got := status.Code(err); got != tc.want {
				t.Errorf("%s / %s: expected %v, got %v", tc.name, rpc, tc.want, got)
			}
		}
	}
}

func TestAnOutageDoesNotLeakTheInternalAddress(t *testing.T) {
	err := newMapsHandler(t, &mapsFake{}).mapMapsError(fmt.Errorf("%w: call OSRM: dial tcp 172.18.0.7:5000: connection refused", maps.ErrUnavailable))

	if message := status.Convert(err).Message(); message != "the map service is not available, try again shortly" {
		t.Errorf("the caller must not see the internal address: %q", message)
	}
}

func TestMapsThatAreNotConfiguredAreUnimplementedAndTheOldConstructorStillWorks(t *testing.T) {
	handler := newMapsHandler(t, nil)
	ctx := context.Background()

	_, routeErr := handler.GetRoute(ctx, &locationv1.GetRouteRequest{Origin: coords(1, 1), Destination: coords(2, 2)})
	_, searchErr := handler.SearchPlaces(ctx, &locationv1.SearchPlacesRequest{Query: "erbil"})
	_, reverseErr := handler.ReverseGeocode(ctx, &locationv1.ReverseGeocodeRequest{Coordinates: coords(1, 1)})

	for rpc, err := range map[string]error{"GetRoute": routeErr, "SearchPlaces": searchErr, "ReverseGeocode": reverseErr} {
		if status.Code(err) != codes.Unimplemented {
			t.Errorf("%s: expected Unimplemented, got %v", rpc, status.Code(err))
		}
	}
}

func TestEveryMapRPCRequiresARequest(t *testing.T) {
	handler := newMapsHandler(t, &mapsFake{})
	ctx := context.Background()

	_, routeErr := handler.GetRoute(ctx, nil)
	_, searchErr := handler.SearchPlaces(ctx, nil)
	_, reverseErr := handler.ReverseGeocode(ctx, nil)

	for rpc, err := range map[string]error{"GetRoute": routeErr, "SearchPlaces": searchErr, "ReverseGeocode": reverseErr} {
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", rpc, status.Code(err))
		}
	}
}

func TestWithMapsRequiresAService(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
		}
	}()

	newMapsHandler(t, nil).WithMaps(nil)
}
