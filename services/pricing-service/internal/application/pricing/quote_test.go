package pricing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const (
	riderA = "11111111-1111-4111-8111-111111111111"
	riderB = "22222222-2222-4222-8222-222222222222"
	tripA  = "33333333-3333-4333-8333-333333333333"
	tripB  = "44444444-4444-4444-8444-444444444444"
)

func quoteInput() QuoteTripInput {
	return QuoteTripInput{RiderID: riderA, PickupLat: 36.19, PickupLng: 44.01, DropoffLat: 36.20, DropoffLng: 44.02}
}

// withComfortCard gives comfort its own, dearer card.
func withComfortCard(h *harness) {
	comfort := h.repo.config
	comfort.ID = "config-comfort"
	comfort.BaseFare = decimal.NewFromInt(3000)
	h.repo.configsByCity["comfort"] = comfort
}

type classAwareRepository struct {
	*fakeRepository
}

func (r classAwareRepository) GetActiveConfig(ctx context.Context, scope Scope) (Config, error) {
	if scope.VehicleClass == VehicleClassComfort {
		if cfg, ok := r.configsByCity["comfort"]; ok {
			return cfg, nil
		}
	}

	return r.fakeRepository.GetActiveConfig(ctx, scope)
}

func TestQuoteTripPricesEveryClassCheapestFirst(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	withComfortCard(h)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	withFrozenTime(t, now)

	svc := NewService(classAwareRepository{h.repo}, h.location, h.routing, h.weather, h.drivers, WithQuoteTTL(3*time.Minute))

	got, err := svc.QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Quotes) != 2 || got.ZoneID != "zone-1" || got.CityID != "city-1" {
		t.Fatalf("got %+v", got)
	}

	economy, comfort := got.Quotes[0], got.Quotes[1]
	if economy.VehicleClass != VehicleClassEconomy || comfort.VehicleClass != VehicleClassComfort {
		t.Fatalf("order: %s, %s", economy.VehicleClass, comfort.VehicleClass)
	}

	if !economy.Breakdown.Total.LessThan(comfort.Breakdown.Total) {
		t.Fatalf("economy %s, comfort %s", economy.Breakdown.Total, comfort.Breakdown.Total)
	}

	if economy.ID == "" || economy.RiderID != riderA || !economy.ExpiresAt.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("economy: %+v", economy)
	}

	if economy.ConfigID != "config-global" || comfort.ConfigID != "config-comfort" {
		t.Fatalf("configs: %s %s", economy.ConfigID, comfort.ConfigID)
	}

	// Only economy drivers are near: comfort has none.
	if !economy.DriversAvailable || economy.PickupETAMinutes != 20 {
		t.Fatalf("economy eta: %v %d", economy.DriversAvailable, economy.PickupETAMinutes)
	}

	if comfort.DriversAvailable || comfort.PickupETAMinutes != 0 {
		t.Fatalf("comfort eta: %v %d", comfort.DriversAvailable, comfort.PickupETAMinutes)
	}

	if h.repo.savedCount != 2 {
		t.Fatalf("saved %d", h.repo.savedCount)
	}
}

func TestQuoteETAFallsBackToStraightLineWhenRoutingFails(t *testing.T) {
	h := newHarness()
	h.routing.err = errors.New("OSRM down")
	h.repo.config.DistanceCorrectionFactor = 1.3
	h.repo.config.AverageSpeedKmh = 30

	got, err := h.service().QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	// ~1.1 km away * 1.3 at 25 km/h is about 4 minutes.
	if eta := got.Quotes[0].PickupETAMinutes; eta < 3 || eta > 5 {
		t.Fatalf("eta %d", eta)
	}
}

func TestQuoteTripRefusals(t *testing.T) {
	h := newHarness()
	svc := h.service()

	bad := quoteInput()
	bad.RiderID = " "
	if _, err := svc.QuoteTrip(context.Background(), bad); !errors.Is(err, ErrRiderIDRequired) {
		t.Fatalf("no rider: %v", err)
	}

	bad = quoteInput()
	bad.DropoffLat = 99
	if _, err := svc.QuoteTrip(context.Background(), bad); !errors.Is(err, ErrInvalidLatitude) {
		t.Fatalf("bad dropoff: %v", err)
	}

	h.location.served = false
	if _, err := svc.QuoteTrip(context.Background(), quoteInput()); !errors.Is(err, ErrPickupOutsideServiceZone) {
		t.Fatalf("outside: %v", err)
	}

	if h.repo.savedCount != 0 {
		t.Fatal("nothing may be saved")
	}
}

// --- surge -------------------------------------------------------------------

func TestDemandSurgeWeighsQuotingRidersAgainstFreeDrivers(t *testing.T) {
	cases := []struct {
		drivers, otherRiders int
		want                 int64
	}{
		{10, 0, 0},
		{10, 14, 15}, // 15 riders per 10 drivers
		{10, 19, 30},
		{10, 29, 50},
		{0, 0, 50},
	}

	for _, tc := range cases {
		h := newHarness()
		h.drivers.drivers = someDrivers(tc.drivers)
		h.repo.quotingRiders = tc.otherRiders

		got, err := h.service().EstimateFare(context.Background(), validEstimateInput())
		if err != nil {
			t.Fatal(err)
		}

		if !got.Surge.DemandPercent.Equal(decimal.NewFromInt(tc.want)) {
			t.Fatalf("%d drivers, %d others: demand %s, want %d", tc.drivers, tc.otherRiders, got.Surge.DemandPercent, tc.want)
		}
	}
}

func TestTheRateCardCanTurnSurgeSourcesOff(t *testing.T) {
	h := newHarness()
	h.drivers.drivers = nil
	h.weather.conditions = WeatherConditions{SurgePercent: decimal.NewFromInt(20)}
	h.repo.config.DemandSurge = false
	h.repo.config.WeatherSurge = false

	got, err := h.service().EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatal(err)
	}

	if !got.Surge.TotalPercent.IsZero() || !got.SurgeAmount.IsZero() {
		t.Fatalf("surge %+v", got.Surge)
	}

	h = newHarness()
	h.drivers.drivers = nil
	h.repo.config.MaxSurgePercent = decimal.Zero

	got, err = h.service().EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatal(err)
	}

	if !got.Surge.TotalPercent.IsZero() || !got.Surge.Multiplier.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("a zero maximum turns surge off: %+v", got.Surge)
	}
}

func TestStaffSurgesTakeTheLargerNotTheSum(t *testing.T) {
	h := newHarness()
	h.repo.surgeRules = []SurgeTimeRule{
		{Label: "Rush hour", StartTime: "00:00:00", EndTime: "23:59:59", SurgePercent: decimal.NewFromInt(25), Active: true},
	}
	h.repo.zoneSurge = ZoneSurge{SurgePercent: decimal.NewFromInt(40), Reason: "Concert"}
	h.repo.zoneSurgeFound = true

	got, err := h.service().EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatal(err)
	}

	if !got.Surge.TotalPercent.Equal(decimal.NewFromInt(40)) || got.Surge.Label != "Concert" ||
		!got.Surge.TimeOfDayPercent.Equal(decimal.NewFromInt(25)) || !got.Surge.ZonePercent.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("surge %+v", got.Surge)
	}

	h.repo.zoneSurge.SurgePercent = decimal.NewFromInt(10)

	got, err = h.service().EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatal(err)
	}

	if !got.Surge.TotalPercent.Equal(decimal.NewFromInt(25)) || got.Surge.Label != "Rush hour" {
		t.Fatalf("surge %+v", got.Surge)
	}
}

func TestSurgeRulesUseTheCitysLocalTimeAndPlace(t *testing.T) {
	// 05:30 UTC is 08:30 in Baghdad (UTC+3).
	withFrozenTime(t, time.Date(2026, 9, 23, 5, 30, 0, 0, time.UTC))

	rush := SurgeTimeRule{Label: "Morning rush", StartTime: "08:00:00", EndTime: "09:00:00", SurgePercent: decimal.NewFromInt(30), Active: true}

	cases := []struct {
		name     string
		timeZone string
		rule     func(SurgeTimeRule) SurgeTimeRule
		want     int64
	}{
		{"everywhere, local time", "Asia/Baghdad", func(r SurgeTimeRule) SurgeTimeRule { return r }, 30},
		{"everywhere, city without a zone read in UTC", "", func(r SurgeTimeRule) SurgeTimeRule { return r }, 0},
		{"the pickup's city", "Asia/Baghdad", func(r SurgeTimeRule) SurgeTimeRule { r.CityID = "city-1"; return r }, 30},
		{"another city", "Asia/Baghdad", func(r SurgeTimeRule) SurgeTimeRule { r.CityID = "city-2"; return r }, 0},
		{"the pickup's zone", "Asia/Baghdad", func(r SurgeTimeRule) SurgeTimeRule { r.ZoneID = "zone-1"; return r }, 30},
		{"another zone", "Asia/Baghdad", func(r SurgeTimeRule) SurgeTimeRule { r.ZoneID = "zone-2"; return r }, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			h.location.timeZone = tc.timeZone
			h.repo.surgeRules = []SurgeTimeRule{tc.rule(rush)}

			got, err := h.service().EstimateFare(context.Background(), validEstimateInput())
			if err != nil {
				t.Fatal(err)
			}

			if !got.Surge.TimeOfDayPercent.Equal(decimal.NewFromInt(tc.want)) {
				t.Fatalf("time surge %s, want %d", got.Surge.TimeOfDayPercent, tc.want)
			}
		})
	}
}

func TestTheRateCardIsLookedUpForTheZoneAndCity(t *testing.T) {
	h := newHarness()
	h.location.zoneID, h.location.cityID = "zone-9", "city-9"
	h.repo.configsByCity["city-9"] = Config{ID: "config-city", CurrencyCode: "IQD", BaseFare: decimal.NewFromInt(7000)}

	got, err := h.service().EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatal(err)
	}

	if h.repo.lastScope != (Scope{ZoneID: "zone-9", CityID: "city-9", VehicleClass: VehicleClassEconomy}) {
		t.Fatalf("scope %+v", h.repo.lastScope)
	}

	if !got.BaseFare.Equal(decimal.NewFromInt(7000)) || got.CityID != "city-9" {
		t.Fatalf("got %+v", got)
	}
}

func TestMinimumFareRaisesTheSubtotal(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.routing.route = Route{DistanceKm: 1, DurationMinutes: 2}
	h.repo.config.MinimumFare = decimal.NewFromInt(3000)

	got, err := h.service().EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatal(err)
	}

	// 1000 + 250 + 200 = 1450, raised by 1550 to 3000.
	if !got.Subtotal.Equal(decimal.NewFromInt(3000)) || !got.MinimumFareAdjustment.Equal(decimal.NewFromInt(1550)) {
		t.Fatalf("subtotal %s, adjustment %s", got.Subtotal, got.MinimumFareAdjustment)
	}
}

// --- claiming ----------------------------------------------------------------

func quotedHarness(t *testing.T) (*harness, Service, Quote) {
	t.Helper()

	h := newHarness()
	h.repo.completedTripCount = 3
	svc := h.service()

	got, err := svc.QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	return h, svc, got.Quotes[0]
}

func TestClaimingAQuote(t *testing.T) {
	_, svc, quote := quotedHarness(t)

	claimed, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA)
	if err != nil || claimed.ClaimedTripID != tripA {
		t.Fatalf("claim: %+v %v", claimed, err)
	}

	if again, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil || again.ID != quote.ID {
		t.Fatalf("claiming again for the same trip: %v", err)
	}

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripB); !errors.Is(err, ErrQuoteAlreadyUsed) {
		t.Fatalf("another trip: %v", err)
	}
}

func TestClaimRefusals(t *testing.T) {
	h, svc, quote := quotedHarness(t)

	cases := map[string]struct {
		quoteID, riderID, tripID string
		want                     error
	}{
		"another rider's": {quote.ID, riderB, tripA, ErrQuoteNotFound},
		"unknown":         {"99999999-9999-4999-8999-999999999999", riderA, tripA, ErrQuoteNotFound},
		"malformed":       {"quote-1", riderA, tripA, ErrQuoteNotFound},
		"no quote":        {" ", riderA, tripA, ErrQuoteIDRequired},
		"no trip":         {quote.ID, riderA, "", ErrTripIDRequired},
	}

	for name, tc := range cases {
		if _, err := svc.ClaimQuote(context.Background(), tc.quoteID, tc.riderID, tc.tripID); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", name, err, tc.want)
		}
	}

	withFrozenTime(t, quote.ExpiresAt.Add(time.Second))

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); !errors.Is(err, ErrQuoteExpired) {
		t.Fatalf("expired: %v", err)
	}

	if h.repo.quotes[quote.ID].ClaimedTripID != "" {
		t.Fatal("a refused claim must not claim")
	}
}

func TestAQuotesCouponMustStillBeUsableWhenClaimed(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["SAVE"] = Coupon{
		ID: "coupon-1", Code: "SAVE", DiscountType: DiscountFixed, DiscountValue: decimal.NewFromInt(500),
		Active: true, ValidFrom: time.Now().Add(-time.Hour), ValidUntil: time.Now().Add(time.Hour), PerRiderLimit: 1,
	}
	svc := h.service()

	input := quoteInput()
	input.CouponCode = "SAVE"

	got, err := svc.QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	quote := got.Quotes[0]
	if quote.Coupon == nil || quote.Coupon.CouponID != "coupon-1" {
		t.Fatalf("coupon: %+v", quote.Coupon)
	}

	// The rider used it on another trip meanwhile.
	h.repo.riderRedemptions["coupon-1"] = 1

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); !errors.Is(err, ErrQuoteCouponUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestReleasingAQuote(t *testing.T) {
	h, svc, quote := quotedHarness(t)

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	if err := svc.ReleaseQuote(context.Background(), quote.ID, tripA); err != nil {
		t.Fatal(err)
	}

	if h.repo.quotes[quote.ID].ClaimedTripID != "" {
		t.Fatal("still claimed")
	}

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripB); err != nil {
		t.Fatalf("a released quote can be claimed again: %v", err)
	}
}

// --- charging the quoted fare --------------------------------------------------

func TestACompletedTripPaysItsQuote(t *testing.T) {
	h, svc, quote := quotedHarness(t)

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	// Prices went up since: the trip still pays what it was quoted.
	h.repo.config.BaseFare = decimal.NewFromInt(99_000)

	fare, err := svc.CalculateFare(context.Background(), CalculateFareInput{TripID: tripA, RiderID: riderA, QuoteID: quote.ID})
	if err != nil {
		t.Fatal(err)
	}

	if !fare.Breakdown.Total.Equal(quote.Breakdown.Total) || fare.QuoteID != quote.ID {
		t.Fatalf("fare %s, quoted %s", fare.Breakdown.Total, quote.Breakdown.Total)
	}

	persisted := h.repo.persistFareCalls[0]
	if persisted.QuoteID != quote.ID || persisted.ConfigID != "config-global" || persisted.RiderID != riderA {
		t.Fatalf("persisted %+v", persisted)
	}
}

func TestAQuoteOnlyPricesTheTripThatClaimedIt(t *testing.T) {
	_, svc, quote := quotedHarness(t)

	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CalculateFare(context.Background(), CalculateFareInput{TripID: tripB, RiderID: riderA, QuoteID: quote.ID}); !errors.Is(err, ErrQuoteNotForTrip) {
		t.Fatalf("got %v", err)
	}
}

type racingRepository struct {
	*fakeRepository
}

func (r racingRepository) PersistFare(ctx context.Context, input PersistFareInput) (Fare, error) {
	_, _ = r.fakeRepository.PersistFare(ctx, input)
	r.existingFare = Fare{TripID: input.TripID, Breakdown: FareBreakdown{Total: decimal.NewFromInt(777)}}
	r.fareFound = true

	return Fare{}, ErrFareAlreadyRecorded
}

func TestAFareRecordedConcurrentlyIsReturned(t *testing.T) {
	h := newHarness()
	svc := NewService(racingRepository{h.repo}, h.location, h.routing, h.weather, h.drivers)

	fare, err := svc.CalculateFare(context.Background(), CalculateFareInput{
		TripID: tripA, RiderID: riderA, PickupLat: 36.19, PickupLng: 44.01, DropoffLat: 36.2, DropoffLng: 44.02,
	})
	if err != nil || !fare.Breakdown.Total.Equal(decimal.NewFromInt(777)) {
		t.Fatalf("got %+v %v", fare, err)
	}
}
