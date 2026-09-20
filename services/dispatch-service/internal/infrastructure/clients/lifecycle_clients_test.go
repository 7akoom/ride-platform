package clients

import (
	"context"
	"testing"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// lifecycleTripFake answers GetTrip and GetActiveTrip; every other method would panic.
type lifecycleTripFake struct {
	tripv1.TripServiceClient

	trip      *tripv1.Trip
	tripErr   error
	activeErr error

	asked *tripv1.GetActiveTripRequest
}

func (f *lifecycleTripFake) GetTrip(_ context.Context, _ *tripv1.GetTripRequest, _ ...grpc.CallOption) (*tripv1.GetTripResponse, error) {
	return &tripv1.GetTripResponse{Trip: f.trip}, f.tripErr
}

func (f *lifecycleTripFake) GetActiveTrip(_ context.Context, in *tripv1.GetActiveTripRequest, _ ...grpc.CallOption) (*tripv1.GetActiveTripResponse, error) {
	f.asked = in

	return &tripv1.GetActiveTripResponse{}, f.activeErr
}

func TestDriverOfTripAnswersTheTripsDriverOrNobody(t *testing.T) {
	client := &TripClient{client: &lifecycleTripFake{trip: &tripv1.Trip{Id: "trip-1", DriverId: "driver-a"}}}

	if got, err := client.DriverOfTrip(context.Background(), "trip-1"); err != nil || got != "driver-a" {
		t.Errorf("got %q, %v", got, err)
	}

	client = &TripClient{client: &lifecycleTripFake{trip: &tripv1.Trip{Id: "trip-1"}}}
	if got, err := client.DriverOfTrip(context.Background(), "trip-1"); err != nil || got != "" {
		t.Errorf("a trip without a driver: got %q, %v", got, err)
	}

	client = &TripClient{client: &lifecycleTripFake{tripErr: status.Error(codes.NotFound, "gone")}}
	if got, err := client.DriverOfTrip(context.Background(), "trip-1"); err != nil || got != "" {
		t.Errorf("a trip that is gone has no driver, and that is not an error: got %q, %v", got, err)
	}

	client = &TripClient{client: &lifecycleTripFake{tripErr: status.Error(codes.Unavailable, "down")}}
	if _, err := client.DriverOfTrip(context.Background(), "trip-1"); err == nil {
		t.Error("an outage must be an error")
	}
}

func TestHasActiveTripReadsTheDriversActiveTrip(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    bool
		wantErr bool
	}{
		{"an active trip", nil, true, false},
		{"none", status.Error(codes.NotFound, "no active trip"), false, false},
		{"an outage", status.Error(codes.Unavailable, "down"), false, true},
		{"trip history not configured", status.Error(codes.Unimplemented, "not configured"), false, true},
	}

	for _, tc := range cases {
		fake := &lifecycleTripFake{activeErr: tc.err}

		got, err := (&TripClient{client: fake}).HasActiveTrip(context.Background(), "driver-a")
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%s: got %v, %v; want %v, error=%v", tc.name, got, err, tc.want, tc.wantErr)
		}

		if fake.asked.GetDriverId() != "driver-a" || fake.asked.GetRiderId() != "" {
			t.Errorf("%s: it must ask about the driver only: %+v", tc.name, fake.asked)
		}
	}
}

type lifecycleDriverFake struct {
	driverv1.DriverServiceClient

	err error
	got *driverv1.UpdateAvailabilityRequest
}

func (f *lifecycleDriverFake) UpdateAvailability(_ context.Context, in *driverv1.UpdateAvailabilityRequest, _ ...grpc.CallOption) (*driverv1.UpdateAvailabilityResponse, error) {
	f.got = in

	return &driverv1.UpdateAvailabilityResponse{}, f.err
}

func TestMarkAvailableAndMarkBusyAreOppositeUpdates(t *testing.T) {
	fake := &lifecycleDriverFake{}
	client := &DriverClient{client: fake}

	if err := client.MarkAvailable(context.Background(), "driver-a"); err != nil {
		t.Fatalf("MarkAvailable: %v", err)
	}

	if fake.got.DriverId != "driver-a" || fake.got.AvailabilityStatus != driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE {
		t.Errorf("MarkAvailable sent %+v", fake.got)
	}

	if err := client.MarkBusy(context.Background(), "driver-a"); err != nil {
		t.Fatalf("MarkBusy: %v", err)
	}

	if fake.got.AvailabilityStatus != driverv1.AvailabilityStatus_AVAILABILITY_STATUS_BUSY {
		t.Errorf("MarkBusy sent %+v", fake.got)
	}

	failing := &DriverClient{client: &lifecycleDriverFake{err: status.Error(codes.Unavailable, "down")}}
	if err := failing.MarkAvailable(context.Background(), "driver-a"); err == nil {
		t.Error("an outage must be an error")
	}
}

// The clients are what the lifecycle handler is built from in main.go.
var (
	_ events.LifecycleTrips   = (*TripClient)(nil)
	_ events.LifecycleDrivers = (*DriverClient)(nil)
)
