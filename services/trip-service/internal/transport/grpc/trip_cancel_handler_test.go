package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// cancelFake records the cancellation it is asked for. The embedded Service
// is nil: any other method would panic.
type cancelFake struct {
	trip.Service

	current trip.Trip
	asked   []trip.CancelInput
}

func (f *cancelFake) GetTrip(context.Context, string) (trip.Trip, error) { return f.current, nil }

func (f *cancelFake) CancelTrip(_ context.Context, input trip.CancelInput) (trip.Trip, error) {
	f.asked = append(f.asked, input)

	return trip.Trip{ID: input.TripID, Status: trip.StatusCancelled, CancelledBy: input.By, RiderNoShow: input.RiderNoShow}, nil
}

func cancelAs(t *testing.T, identity string, resolver CallerResolver, request *tripv1.CancelTripRequest) (*cancelFake, *tripv1.CancelTripResponse, error) {
	t.Helper()

	fake := &cancelFake{current: trip.Trip{ID: "trip-1", RiderID: "rider-a", DriverID: "driver-a", Status: trip.StatusAccepted}}

	var options []HandlerOption
	if resolver != nil {
		options = append(options, WithParticipants(resolver))
	}

	handler := NewTripHandler(fake, slog.New(slog.NewTextHandler(io.Discard, nil)), options...)
	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: identity, SessionID: identity})

	response, err := handler.CancelTrip(ctx, request)

	return fake, response, err
}

func TestTheCancellationSaysWhoCancelled(t *testing.T) {
	resolver := ownershipTestResolver{
		riders:  map[string]string{"id-rider": "rider-a", "id-both": "rider-z"},
		drivers: map[string]string{"id-driver": "driver-a", "id-both": "driver-a"},
	}

	cases := map[string]trip.CancelledBy{
		"id-rider":                 trip.CancelledByRider,
		"id-driver":                trip.CancelledByDriver,
		"id-both":                  trip.CancelledByDriver,
		internalServicePrincipalID: trip.CancelledBySystem,
	}

	for identity, want := range cases {
		fake, response, err := cancelAs(t, identity, resolver, &tripv1.CancelTripRequest{TripId: "trip-1", Reason: "changed plans"})
		if err != nil {
			t.Fatalf("%s: %v", identity, err)
		}

		if fake.asked[0].By != want || response.GetTrip().GetCancelledBy() != string(want) {
			t.Fatalf("%s: asked %+v", identity, fake.asked[0])
		}
	}
}

func TestADriversNoShowIsPassedOn(t *testing.T) {
	resolver := ownershipTestResolver{drivers: map[string]string{"id-driver": "driver-a"}}

	fake, response, err := cancelAs(t, "id-driver", resolver, &tripv1.CancelTripRequest{TripId: "trip-1", RiderNoShow: true})
	if err != nil || !fake.asked[0].RiderNoShow || !response.GetTrip().GetRiderNoShow() {
		t.Fatalf("asked %+v, %v", fake.asked, err)
	}
}

func TestACancellerThatCannotBeToldApartIsRefused(t *testing.T) {
	if _, _, err := cancelAs(t, "id-stranger", ownershipTestResolver{}, &tripv1.CancelTripRequest{TripId: "trip-1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a stranger: %v", err)
	}

	if _, _, err := cancelAs(t, "id-rider", ownershipTestResolver{err: errors.New("rider-service down")}, &tripv1.CancelTripRequest{TripId: "trip-1"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("resolver down: %v", err)
	}

	fake, _, err := cancelAs(t, "id-rider", nil, &tripv1.CancelTripRequest{TripId: "trip-1"})
	if status.Code(err) != codes.Internal || len(fake.asked) != 0 {
		t.Fatalf("no resolver: %v", err)
	}
}
