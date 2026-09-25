package grpc

import (
	"io"
	"log/slog"
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestOnlyTheDriverOfTheTripMarksItsStops(t *testing.T) {
	request := &tripv1.ReachStopRequest{TripId: "trip-1", Position: 1}

	runOwnershipCases(t, codes.OK, []ownershipCase{
		{"the driver", "id-driver-a", "ReachStop", request},
	})

	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"the rider", "id-rider-a", "ReachStop", request},
		{"a stranger", "id-rider-b", "ReachStop", request},
	})
}

func TestStopsGoInAndOutInOrder(t *testing.T) {
	reached := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	in := toDomainStops([]*tripv1.TripStop{
		{Coordinates: &tripv1.Coordinates{Latitude: 36.15, Longitude: 44.15}, Address: "Bakery", ReachedAt: nil},
		{Coordinates: &tripv1.Coordinates{Latitude: 36.17, Longitude: 44.17}, Address: "Pharmacy"},
	})
	if len(in) != 2 || in[1].Address != "Pharmacy" || in[0].Coordinates.Latitude != 36.15 {
		t.Fatalf("in %+v", in)
	}

	in[0].ReachedAt = &reached
	out := toProtoTrip(trip.Trip{Status: trip.StatusInProgress, Stops: in}).GetStops()

	if len(out) != 2 || !out[0].GetReachedAt().AsTime().Equal(reached) || out[1].GetReachedAt() != nil || out[1].GetAddress() != "Pharmacy" {
		t.Fatalf("out %+v", out)
	}

	offer := toProtoOffer(trip.Offer{Trip: trip.Trip{Stops: in}})
	if len(offer.GetStops()) != 2 {
		t.Fatalf("the offer shows %d stops", len(offer.GetStops()))
	}
}

func TestStopErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &TripHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		trip.ErrTooManyStops:   codes.InvalidArgument,
		trip.ErrStopNotFound:   codes.NotFound,
		trip.ErrTooFarFromStop: codes.FailedPrecondition,
	} {
		if got := status.Code(handler.mapTripError(err)); got != want {
			t.Errorf("%v: got %v, want %v", err, got, want)
		}
	}
}
