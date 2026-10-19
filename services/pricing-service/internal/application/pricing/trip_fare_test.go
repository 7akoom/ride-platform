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
