package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// handlerHistoryFake is a trip.Service that can also serve history. The embedded
// Service is left nil: any other method would panic.
type handlerHistoryFake struct {
	trip.Service

	active trip.Trip
	page   trip.HistoryPage
	err    error

	activeAsked [][2]string
	listAsked   []trip.HistoryQuery
}

func (f *handlerHistoryFake) GetActiveTrip(_ context.Context, riderID string, driverID string) (trip.Trip, error) {
	f.activeAsked = append(f.activeAsked, [2]string{riderID, driverID})

	return f.active, f.err
}

func (f *handlerHistoryFake) ListTrips(_ context.Context, query trip.HistoryQuery) (trip.HistoryPage, error) {
	f.listAsked = append(f.listAsked, query)

	return f.page, f.err
}

func newHistoryHandler(service trip.Service) *TripHandler {
	return NewTripHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestGetActiveTripReturnsTheTripAndAsksForTheProfileGiven(t *testing.T) {
	fake := &handlerHistoryFake{active: trip.Trip{ID: "trip-1", RiderID: "rider-a"}}

	response, err := newHistoryHandler(fake).GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{DriverId: "driver-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetTrip().GetId() != "trip-1" {
		t.Errorf("wrong trip: %+v", response.GetTrip())
	}

	if len(fake.activeAsked) != 1 || fake.activeAsked[0] != [2]string{"", "driver-a"} {
		t.Errorf("unexpected question: %v", fake.activeAsked)
	}
}

func TestGetActiveTripMapsItsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"no active trip", trip.ErrTripNotFound, codes.NotFound},
		{"a query that names both or neither", trip.ErrInvalidHistoryQuery, codes.InvalidArgument},
		{"a wrapped not-found", errors.Join(errors.New("context"), trip.ErrTripNotFound), codes.NotFound},
		{"anything else", errors.New("database down"), codes.Internal},
	}

	for _, tc := range cases {
		_, err := newHistoryHandler(&handlerHistoryFake{err: tc.err}).GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{RiderId: "rider-a"})

		if got := status.Code(err); got != tc.want {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestNoActiveTripSaysSo(t *testing.T) {
	_, err := newHistoryHandler(&handlerHistoryFake{err: trip.ErrTripNotFound}).GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{RiderId: "rider-a"})

	if status.Convert(err).Message() != "no active trip" {
		t.Errorf("unexpected message: %q", status.Convert(err).Message())
	}
}

func TestListTripsReturnsThePageAndPassesTheRequestThrough(t *testing.T) {
	fake := &handlerHistoryFake{page: trip.HistoryPage{
		Trips:         []trip.Trip{{ID: "trip-2", RiderID: "rider-a"}, {ID: "trip-1", RiderID: "rider-a"}},
		NextPageToken: "next",
	}}

	response, err := newHistoryHandler(fake).ListTrips(context.Background(), &tripv1.ListTripsRequest{RiderId: "rider-a", PageSize: 2, PageToken: "prev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(response.GetTrips()) != 2 || response.GetTrips()[0].GetId() != "trip-2" || response.GetTrips()[1].GetId() != "trip-1" {
		t.Errorf("wrong trips, or wrong order: %+v", response.GetTrips())
	}

	if response.GetNextPageToken() != "next" {
		t.Errorf("wrong next page token: %q", response.GetNextPageToken())
	}

	want := trip.HistoryQuery{RiderID: "rider-a", PageSize: 2, PageToken: "prev"}
	if len(fake.listAsked) != 1 || fake.listAsked[0] != want {
		t.Errorf("unexpected query: %+v", fake.listAsked)
	}
}

func TestAnEmptyHistoryIsAnEmptyListNotAnError(t *testing.T) {
	response, err := newHistoryHandler(&handlerHistoryFake{}).ListTrips(context.Background(), &tripv1.ListTripsRequest{DriverId: "driver-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetTrips() == nil || len(response.GetTrips()) != 0 || response.GetNextPageToken() != "" {
		t.Errorf("expected an empty, non-nil list: %+v", response)
	}
}

func TestListTripsMapsItsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"a bad page token", trip.ErrInvalidPageToken, codes.InvalidArgument},
		{"a negative page size", trip.ErrInvalidPageSize, codes.InvalidArgument},
		{"a query that names both or neither", trip.ErrInvalidHistoryQuery, codes.InvalidArgument},
		{"a wrapped bad token", errors.Join(errors.New("context"), trip.ErrInvalidPageToken), codes.InvalidArgument},
		{"anything else", errors.New("database down"), codes.Internal},
	}

	for _, tc := range cases {
		_, err := newHistoryHandler(&handlerHistoryFake{err: tc.err}).ListTrips(context.Background(), &tripv1.ListTripsRequest{RiderId: "rider-a"})

		if got := status.Code(err); got != tc.want {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestHistoryWithoutItConfiguredIsUnimplemented(t *testing.T) {
	// A trip.Service that was not wrapped with trip.WithTripHistory.
	plain := struct{ trip.Service }{}
	handler := newHistoryHandler(plain)

	if _, err := handler.GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{RiderId: "rider-a"}); status.Code(err) != codes.Unimplemented {
		t.Errorf("GetActiveTrip: expected Unimplemented, got %v", status.Code(err))
	}

	if _, err := handler.ListTrips(context.Background(), &tripv1.ListTripsRequest{RiderId: "rider-a"}); status.Code(err) != codes.Unimplemented {
		t.Errorf("ListTrips: expected Unimplemented, got %v", status.Code(err))
	}
}

func TestHistoryIsFoundThroughOtherDecorators(t *testing.T) {
	// The history capability sits behind a layer that says what it wraps, as
	// WithDriverTracking does when it wraps WithTripHistory.
	stacked := &wrappedHistoryLayer{Service: &handlerHistoryFake{
		active: trip.Trip{ID: "trip-active"},
		page:   trip.HistoryPage{Trips: []trip.Trip{{ID: "trip-1"}}},
	}}

	handler := newHistoryHandler(stacked)

	list, err := handler.ListTrips(context.Background(), &tripv1.ListTripsRequest{RiderId: "rider-a"})
	if err != nil || len(list.GetTrips()) != 1 {
		t.Errorf("ListTrips behind another decorator must still be served: %+v, %v", list, err)
	}

	active, err := handler.GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{RiderId: "rider-a"})
	if err != nil || active.GetTrip().GetId() != "trip-active" {
		t.Errorf("GetActiveTrip behind another decorator must still be served: %+v, %v", active, err)
	}
}

type wrappedHistoryLayer struct{ trip.Service }

func (l *wrappedHistoryLayer) Unwrap() trip.Service { return l.Service }

func TestHistoryRequiresARequest(t *testing.T) {
	handler := newHistoryHandler(&handlerHistoryFake{})

	if _, err := handler.GetActiveTrip(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Errorf("GetActiveTrip: expected InvalidArgument, got %v", status.Code(err))
	}

	if _, err := handler.ListTrips(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Errorf("ListTrips: expected InvalidArgument, got %v", status.Code(err))
	}
}
