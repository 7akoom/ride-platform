package pricing

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

type tripReaderFake struct {
	trip TripSnapshot
	err  error
}

func (f tripReaderFake) TripSnapshot(context.Context, string) (TripSnapshot, error) {
	return f.trip, f.err
}

func termsService(t *testing.T, trip TripSnapshot) *service {
	t.Helper()

	h := newHarness()
	card := feeCard()
	h.repo.config.FreeWaitingMinutes = card.FreeWaitingMinutes
	h.repo.config.WaitingPerMinute = card.WaitingPerMinute
	h.repo.config.CancellationFee = card.CancellationFee
	h.repo.config.CancellationGraceMinutes = card.CancellationGraceMinutes
	h.repo.config.NoShowFee = card.NoShowFee

	return h.service(WithTripReader(tripReaderFake{trip: trip})).(*service)
}

func snapshot(accepted, arrived int) TripSnapshot {
	trip := TripSnapshot{RiderID: riderA, DriverID: "driver-1", VehicleClass: "economy", PickupLat: 36.19, PickupLng: 44.01}
	if accepted >= 0 {
		trip.AcceptedAt = at(accepted)
	}

	if arrived >= 0 {
		trip.ArrivedAt = at(arrived)
	}

	return trip
}

func TestTripTerms_TheCardsTermsAndWhatAppliesNow(t *testing.T) {
	svc := termsService(t, snapshot(0, 4))

	terms, err := svc.TripTerms(context.Background(), riderA, tripA, *at(5))
	if err != nil {
		t.Fatal(err)
	}

	if terms.FreeWaitingMinutes != 3 || !terms.WaitingPerMinute.Equal(decimal.NewFromInt(100)) ||
		!terms.CancellationFee.Equal(decimal.NewFromInt(1000)) || terms.CancellationGraceMinutes != 2 ||
		!terms.NoShowFee.Equal(decimal.NewFromInt(2000)) || terms.NoShowWaitMinutes != DefaultNoShowWaitMinutes {
		t.Fatalf("terms %+v", terms)
	}

	if !terms.CancellationFeeFrom.Equal(*at(2)) || !terms.CancellationFeeNow.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("fee from %v, now %s; want minute 2 and 1000", terms.CancellationFeeFrom, terms.CancellationFeeNow)
	}

	if !terms.NoShowFrom.Equal(*at(4 + DefaultNoShowWaitMinutes)) {
		t.Fatalf("no-show from %v", terms.NoShowFrom)
	}
}

func TestTripTerms_NoFeeInTheGraceMinutesBeforeAcceptingOrWithTheDriverLate(t *testing.T) {
	cases := map[string]struct {
		trip TripSnapshot
		now  int
	}{
		"within the grace minutes": {snapshot(0, -1), 1},
		"no driver yet":            {TripSnapshot{RiderID: riderA}, 30},
		"the driver is late":       {snapshot(0, -1), 16},
	}

	for name, tc := range cases {
		terms, err := termsService(t, tc.trip).TripTerms(context.Background(), riderA, tripA, *at(tc.now))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if !terms.CancellationFeeNow.IsZero() {
			t.Fatalf("%s: fee now %s, want 0", name, terms.CancellationFeeNow)
		}
	}
}

func TestTripTerms_AnotherRidersTripIsNotFound(t *testing.T) {
	_, err := termsService(t, snapshot(0, -1)).TripTerms(context.Background(), "someone-else", tripA, *at(5))
	if !errors.Is(err, ErrTripTermsNotFound) {
		t.Fatalf("err = %v, want ErrTripTermsNotFound", err)
	}
}

func TestNoShowWaitOrDefault(t *testing.T) {
	if NoShowWaitOrDefault(0) != 6 || NoShowWaitOrDefault(-1) != 6 || NoShowWaitOrDefault(9) != 9 {
		t.Fatal("unexpected no-show wait")
	}
}
