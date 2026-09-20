package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// handlerTrackingFake is a trip.Service that can also track drivers. The
// embedded Service is left nil: any other method would panic.
type handlerTrackingFake struct {
	trip.Service

	location trip.DriverLocation
	err      error
	asked    []string
}

func (f *handlerTrackingFake) GetDriverLocation(_ context.Context, tripID string) (trip.DriverLocation, error) {
	f.asked = append(f.asked, tripID)

	return f.location, f.err
}

func newTrackingHandler(service trip.Service) *TripHandler {
	return NewTripHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestGetDriverLocationReturnsThePositionAndWhenItWasReported(t *testing.T) {
	reported := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	fake := &handlerTrackingFake{location: trip.DriverLocation{Latitude: 36.1905, Longitude: 44.0105, UpdatedAt: reported}}

	response, err := newTrackingHandler(fake).GetDriverLocation(context.Background(), &tripv1.GetDriverLocationRequest{TripId: "trip-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetLocation().GetLatitude() != 36.1905 || response.GetLocation().GetLongitude() != 44.0105 {
		t.Errorf("wrong location: %+v", response.GetLocation())
	}

	if !response.GetUpdatedAt().AsTime().Equal(reported) {
		t.Errorf("wrong updated_at: %v", response.GetUpdatedAt().AsTime())
	}

	if len(fake.asked) != 1 || fake.asked[0] != "trip-1" {
		t.Errorf("expected one lookup of trip-1, got %v", fake.asked)
	}
}

func TestGetDriverLocationMapsItsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"a trip that is not active", trip.ErrTripNotTrackable, codes.FailedPrecondition},
		{"a driver who has not reported", trip.ErrDriverLocationUnavailable, codes.NotFound},
		{"a trip that does not exist", trip.ErrTripNotFound, codes.NotFound},
		{"a wrapped not-active error", errors.Join(errors.New("context"), trip.ErrTripNotTrackable), codes.FailedPrecondition},
		{"anything else", errors.New("location-service down"), codes.Internal},
	}

	for _, tc := range cases {
		_, err := newTrackingHandler(&handlerTrackingFake{err: tc.err}).GetDriverLocation(context.Background(), &tripv1.GetDriverLocationRequest{TripId: "trip-1"})

		if got := status.Code(err); got != tc.want {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestGetDriverLocationWithoutTrackingConfiguredIsUnimplemented(t *testing.T) {
	// A trip.Service that was not wrapped with trip.WithDriverTracking.
	plain := struct{ trip.Service }{}

	_, err := newTrackingHandler(plain).GetDriverLocation(context.Background(), &tripv1.GetDriverLocationRequest{TripId: "trip-1"})

	if status.Code(err) != codes.Unimplemented {
		t.Errorf("expected Unimplemented, got %v", status.Code(err))
	}
}

func TestGetDriverLocationRequiresARequest(t *testing.T) {
	_, err := newTrackingHandler(&handlerTrackingFake{}).GetDriverLocation(context.Background(), nil)

	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", status.Code(err))
	}
}
