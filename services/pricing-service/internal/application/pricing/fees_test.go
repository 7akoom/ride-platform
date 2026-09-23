package pricing

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func at(minutes int) *time.Time {
	t := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
	return &t
}

func feeCard() Config {
	return Config{
		ID: "config-fees", CurrencyCode: "IQD",
		FreeWaitingMinutes: 3, WaitingPerMinute: decimal.NewFromInt(100),
		CancellationFee: decimal.NewFromInt(1000), CancellationGraceMinutes: 2,
		NoShowFee: decimal.NewFromInt(2000),
	}
}

func TestWaitingBeyondTheFreeMinutesIsCharged(t *testing.T) {
	cases := []struct {
		name           string
		arrived, start *time.Time
		minutes        int
		fare, total    int64
	}{
		{"waited 10 minutes", at(0), at(10), 7, 700, 4750},
		{"part of a minute does not count", at(0), func() *time.Time { t := at(5).Add(50 * time.Second); return &t }(), 2, 200, 4250},
		{"within the free minutes", at(0), at(3), 0, 0, 4000},
		{"never marked arrival", nil, at(10), 0, 0, 4000},
	}

	for _, tc := range cases {
		b := FareBreakdown{Total: decimal.NewFromInt(4000)}
		addWaiting(&b, feeCard(), tc.arrived, tc.start, decimal.NewFromInt(250))

		if b.WaitingMinutes != tc.minutes || !b.WaitingFare.Equal(decimal.NewFromInt(tc.fare)) || !b.Total.Equal(decimal.NewFromInt(tc.total)) {
			t.Fatalf("%s: %d minutes, %s, total %s", tc.name, b.WaitingMinutes, b.WaitingFare, b.Total)
		}
	}
}

func TestAQuotedTripPaysItsQuotePlusWaiting(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.config.FreeWaitingMinutes = 3
	h.repo.config.WaitingPerMinute = decimal.NewFromInt(100)
	svc := h.service(WithFareRounding(decimal.NewFromInt(250)))

	quotes, err := svc.QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	quote := quotes.Quotes[0]
	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	fare, err := svc.CalculateFare(context.Background(), CalculateFareInput{
		TripID: tripA, RiderID: riderA, QuoteID: quote.ID, ArrivedAt: at(0), StartedAt: at(8),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := quote.Breakdown.Total.Add(decimal.NewFromInt(500))
	if fare.Breakdown.WaitingMinutes != 5 || !fare.Breakdown.Total.Equal(want) || fare.Kind != FareKindTrip {
		t.Fatalf("fare %+v, want total %s", fare.Breakdown, want)
	}
}

func cancelled(by string, noShow bool, accepted, arrived, cancelledAt *time.Time) CancellationInput {
	return CancellationInput{
		TripID: tripA, RiderID: riderA, DriverID: "driver-1", VehicleClass: "economy",
		PickupLat: 36.19, PickupLng: 44.01,
		CancelledBy: by, RiderNoShow: noShow, AcceptedAt: accepted, ArrivedAt: arrived, CancelledAt: cancelledAt,
	}
}

func TestWhoPaysForACancellation(t *testing.T) {
	cases := []struct {
		name  string
		input CancellationInput
		kind  FareKind
		total int64
	}{
		{"rider, within the grace minutes", cancelled("rider", false, at(0), nil, at(1)), "", 0},
		{"rider, after the grace minutes", cancelled("rider", false, at(0), nil, at(5)), FareKindCancellation, 1000},
		{"rider, the driver is late", cancelled("rider", false, at(0), nil, at(20)), "", 0},
		{"rider, the driver arrived long ago", cancelled("rider", false, at(0), at(5), at(25)), FareKindCancellation, 1000},
		{"rider, before anyone accepted", cancelled("rider", false, nil, nil, at(5)), "", 0},
		{"driver, the rider did not come", cancelled("driver", true, at(0), at(5), at(11)), FareKindNoShow, 2000},
		{"driver, for another reason", cancelled("driver", false, at(0), at(5), at(11)), "", 0},
		{"system", cancelled("system", false, at(0), nil, at(5)), "", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			card := feeCard()
			card.MaxSurgePercent = decimal.NewFromInt(150)
			h.repo.config = card

			fare, charged, err := h.service(WithFareRounding(decimal.NewFromInt(250))).ChargeCancellation(context.Background(), tc.input)
			if err != nil {
				t.Fatal(err)
			}

			if charged != (tc.kind != "") {
				t.Fatalf("charged %v", charged)
			}

			if !charged {
				if len(h.repo.persistFareCalls) != 0 {
					t.Fatal("nothing may be recorded")
				}

				return
			}

			if fare.Kind != tc.kind || !fare.Breakdown.Total.Equal(decimal.NewFromInt(tc.total)) {
				t.Fatalf("fare %s %s", fare.Kind, fare.Breakdown.Total)
			}

			persisted := h.repo.persistFareCalls[0]
			if persisted.Kind != tc.kind || persisted.ConfigID != "config-fees" || persisted.Coupon != nil || persisted.RiderID != riderA {
				t.Fatalf("persisted %+v", persisted)
			}
		})
	}
}

func TestAZeroFeeChargesNothing(t *testing.T) {
	h := newHarness()
	h.repo.config.CancellationGraceMinutes = 2

	if _, charged, err := h.service().ChargeCancellation(context.Background(), cancelled("rider", false, at(0), nil, at(5))); err != nil || charged {
		t.Fatalf("charged %v, %v", charged, err)
	}
}

func TestAQuotedTripsFeeComesFromItsQuotesCard(t *testing.T) {
	h, svc, quote := quotedHarness(t)

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	// The quote was priced with config-global; the card in force now charges
	// a different fee.
	h.repo.config.CancellationFee = decimal.NewFromInt(1500)
	quoted := h.repo.config
	h.repo.config = feeCard()
	h.repo.configsByCity["other"] = quoted

	input := cancelled("rider", false, at(0), at(3), at(10))
	input.QuoteID = quote.ID

	fare, charged, err := svc.ChargeCancellation(context.Background(), input)
	if err != nil || !charged || !fare.Breakdown.Total.Equal(decimal.NewFromInt(1500)) {
		t.Fatalf("fare %+v %v %v", fare.Breakdown, charged, err)
	}
}

func TestAFeeIsChargedOnce(t *testing.T) {
	h := newHarness()
	h.repo.config = feeCard()
	h.repo.existingFare = Fare{TripID: tripA, Kind: FareKindCancellation}
	h.repo.fareFound = true

	fare, charged, err := h.service().ChargeCancellation(context.Background(), cancelled("rider", false, at(0), nil, at(5)))
	if err != nil || !charged || fare.Kind != FareKindCancellation || len(h.repo.persistFareCalls) != 0 {
		t.Fatalf("%+v %v %v", fare, charged, err)
	}
}
