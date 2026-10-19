package pricing

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

const (
	tripFareTrip  = "6f0c2a1e-0b7d-4c55-9a3e-1f2d3c4b5a69"
	tripFareRider = "rider-1"
)

func TestTripFareIsTheRidersOwnRecordedFare(t *testing.T) {
	h := newHarness()
	h.repo.existingFare = Fare{TripID: tripFareTrip, RiderID: tripFareRider, Kind: FareKindTrip,
		Breakdown: FareBreakdown{Total: decimal.NewFromInt(4000)}}
	h.repo.fareFound = true

	fare, err := h.service().TripFare(context.Background(), tripFareRider, tripFareTrip)
	if err != nil || !fare.Breakdown.Total.Equal(decimal.NewFromInt(4000)) {
		t.Fatalf("fare %+v, err %v", fare.Breakdown, err)
	}
}

func TestTripFareOfAnotherRiderIsNotFound(t *testing.T) {
	h := newHarness()
	h.repo.existingFare = Fare{TripID: tripFareTrip, RiderID: "rider-2"}
	h.repo.fareFound = true

	if _, err := h.service().TripFare(context.Background(), tripFareRider, tripFareTrip); !errors.Is(err, ErrFareNotFound) {
		t.Fatalf("got %v, want ErrFareNotFound", err)
	}
}

func TestTripFareWithoutAFareIsNotFound(t *testing.T) {
	h := newHarness()

	if _, err := h.service().TripFare(context.Background(), tripFareRider, tripFareTrip); !errors.Is(err, ErrFareNotFound) {
		t.Fatalf("got %v, want ErrFareNotFound", err)
	}
}

func TestTripFareChecksItsInput(t *testing.T) {
	svc := newHarness().service()

	if _, err := svc.TripFare(context.Background(), " ", tripFareTrip); !errors.Is(err, ErrRiderIDRequired) {
		t.Fatalf("no rider: got %v", err)
	}

	if _, err := svc.TripFare(context.Background(), tripFareRider, ""); !errors.Is(err, ErrTripIDRequired) {
		t.Fatalf("no trip: got %v", err)
	}

	if _, err := svc.TripFare(context.Background(), tripFareRider, "not-a-trip-id"); !errors.Is(err, ErrFareNotFound) {
		t.Fatalf("not a uuid: got %v", err)
	}
}

func TestTripFaresAsksOnceForEachRealTripID(t *testing.T) {
	h := newHarness()
	h.repo.existingFare = Fare{TripID: tripFareTrip, RiderID: tripFareRider, Breakdown: FareBreakdown{Total: decimal.NewFromInt(3500)}}
	h.repo.fareFound = true

	fares, err := h.service().TripFares(context.Background(), []string{tripFareTrip, " " + tripFareTrip, "nope", ""})
	if err != nil || len(fares) != 1 || !fares[0].Breakdown.Total.Equal(decimal.NewFromInt(3500)) {
		t.Fatalf("fares %+v, err %v", fares, err)
	}

	if len(h.repo.listedFareIDs) != 1 || h.repo.listedFareIDs[0] != tripFareTrip {
		t.Fatalf("asked for %v, want only %s", h.repo.listedFareIDs, tripFareTrip)
	}
}

func TestTripFaresRefusesTooManyTrips(t *testing.T) {
	ids := make([]string, MaxTripFares+1)
	for i := range ids {
		ids[i] = tripFareTrip
	}

	if _, err := newHarness().service().TripFares(context.Background(), ids); !errors.Is(err, ErrTooManyTrips) {
		t.Fatalf("got %v, want ErrTooManyTrips", err)
	}
}
