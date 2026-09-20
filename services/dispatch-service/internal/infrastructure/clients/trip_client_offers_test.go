package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// offerClientFake answers OfferTrip; every other method would panic.
type offerClientFake struct {
	tripv1.TripServiceClient

	err error
	got *tripv1.OfferTripRequest
}

func (f *offerClientFake) OfferTrip(_ context.Context, in *tripv1.OfferTripRequest, _ ...grpc.CallOption) (*tripv1.OfferTripResponse, error) {
	f.got = in

	return &tripv1.OfferTripResponse{}, f.err
}

func TestOfferTripSendsTheTripTheDriverAndTheTTLInSeconds(t *testing.T) {
	fake := &offerClientFake{}

	if err := (&TripClient{client: fake}).OfferTrip(context.Background(), "trip-1", "driver-a", 20*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fake.got.GetTripId() != "trip-1" || fake.got.GetDriverId() != "driver-a" || fake.got.GetTtlSeconds() != 20 {
		t.Errorf("unexpected request: %+v", fake.got)
	}
}

func TestEachRefusalOfTripServiceBecomesWhatDispatchActsOn(t *testing.T) {
	cases := []struct {
		name string
		code codes.Code
		is   error
		not  error
	}{
		{"another driver's offer is live", codes.Aborted, dispatch.ErrOfferPending, dispatch.ErrDriverNotOfferable},
		{"offered to this driver before", codes.AlreadyExists, dispatch.ErrDriverNotOfferable, dispatch.ErrOfferPending},
		{"the driver has an offer or a trip", codes.FailedPrecondition, dispatch.ErrDriverNotOfferable, dispatch.ErrOfferPending},
		{"the trip does not exist", codes.NotFound, nil, dispatch.ErrOfferPending},
		{"trip-service is down", codes.Unavailable, nil, dispatch.ErrDriverNotOfferable},
		{"permission denied", codes.PermissionDenied, nil, dispatch.ErrDriverNotOfferable},
	}

	for _, tc := range cases {
		err := (&TripClient{client: &offerClientFake{err: status.Error(tc.code, "refused")}}).OfferTrip(context.Background(), "trip-1", "driver-a", time.Minute)

		if err == nil {
			t.Errorf("%s: expected an error", tc.name)

			continue
		}

		if tc.is != nil && !errors.Is(err, tc.is) {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.is, err)
		}

		if errors.Is(err, tc.not) {
			t.Errorf("%s: must not be %v", tc.name, tc.not)
		}

		if tc.is == nil && errors.Is(err, dispatch.ErrOfferPending) {
			t.Errorf("%s: an ordinary failure must not look like a pending offer", tc.name)
		}
	}
}

func TestTheTripClientCanOfferTrips(t *testing.T) {
	// dispatch.WithOffers panics at construction if this stops being true.
	var _ dispatch.TripOfferer = (*TripClient)(nil)
}
