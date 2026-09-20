package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// driverLocationFakeClient answers GetLocation only; the embedded interface is
// left nil so a call to anything else panics.
type driverLocationFakeClient struct {
	locationv1.LocationServiceClient

	response *locationv1.GetLocationResponse
	err      error
	request  *locationv1.GetLocationRequest
}

func (f *driverLocationFakeClient) GetLocation(
	_ context.Context,
	request *locationv1.GetLocationRequest,
	_ ...grpc.CallOption,
) (*locationv1.GetLocationResponse, error) {
	f.request = request

	return f.response, f.err
}

func TestDriverLocationAsksForTheDriverAndMapsTheAnswer(t *testing.T) {
	reported := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	fake := &driverLocationFakeClient{response: &locationv1.GetLocationResponse{
		Coordinates: &locationv1.Coordinates{Latitude: 36.1905, Longitude: 44.0105},
		UpdatedAt:   timestamppb.New(reported),
	}}

	got, err := (&LocationClient{client: fake}).DriverLocation(context.Background(), "driver-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fake.request.GetEntityType() != locationv1.EntityType_ENTITY_TYPE_DRIVER || fake.request.GetEntityId() != "driver-a" {
		t.Errorf("wrong request: %+v", fake.request)
	}

	want := trip.DriverLocation{Latitude: 36.1905, Longitude: 44.0105, UpdatedAt: reported}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAMissingPositionIsTheExpectedUnavailableAnswer(t *testing.T) {
	fake := &driverLocationFakeClient{err: status.Error(codes.NotFound, "location not found or stale")}

	_, err := (&LocationClient{client: fake}).DriverLocation(context.Background(), "driver-a")
	if !errors.Is(err, trip.ErrDriverLocationUnavailable) {
		t.Errorf("expected ErrDriverLocationUnavailable, got %v", err)
	}
}

func TestAnyOtherFailureIsReportedAsAnError(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.PermissionDenied, codes.Internal} {
		fake := &driverLocationFakeClient{err: status.Error(code, "boom")}

		_, err := (&LocationClient{client: fake}).DriverLocation(context.Background(), "driver-a")
		if err == nil || errors.Is(err, trip.ErrDriverLocationUnavailable) {
			t.Errorf("%v: expected a real error, got %v", code, err)
		}
	}
}
