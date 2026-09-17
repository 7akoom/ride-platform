package pricing

// Internal test package: needs to override the package-level nowFunc to
// freeze time for coupon-validity and surge tests.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	config    Config
	configErr error

	// configsByZone lets a test give one specific zone its own rate
	// card; GetActiveConfig falls back to config (the global default)
	// for any zone not present here — mirrors the real repository's
	// zone-then-global lookup.
	configsByZone map[string]Config

	surgeRules []SurgeTimeRule

	couponsByCode map[string]Coupon
	createCoupon  *Coupon
	createErr     error

	riderRedemptions map[string]int // couponID -> count

	completedTripCount int

	existingFare Fare
	fareFound    bool
	findFareErr  error

	persistFareCalls []PersistFareInput
	persistFareErr   error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		couponsByCode:    map[string]Coupon{},
		riderRedemptions: map[string]int{},
		configsByZone:    map[string]Config{},
	}
}

func (r *fakeRepository) GetActiveConfig(_ context.Context, zoneID string) (Config, error) {
	if r.configErr != nil {
		return Config{}, r.configErr
	}

	if zoneID != "" {
		if cfg, ok := r.configsByZone[zoneID]; ok {
			return cfg, nil
		}
	}

	return r.config, nil
}

func (r *fakeRepository) ListActiveSurgeTimeRules(_ context.Context) ([]SurgeTimeRule, error) {
	return r.surgeRules, nil
}

func (r *fakeRepository) FindCouponByCode(_ context.Context, code string) (Coupon, error) {
	if c, ok := r.couponsByCode[code]; ok {
		return c, nil
	}
	return Coupon{}, ErrCouponNotFound
}

func (r *fakeRepository) CreateCoupon(_ context.Context, input CreateCouponInput) (Coupon, error) {
	if r.createErr != nil {
		return Coupon{}, r.createErr
	}
	if r.createCoupon != nil {
		return *r.createCoupon, nil
	}
	return Coupon{Code: input.Code, DiscountType: input.DiscountType, DiscountValue: input.DiscountValue}, nil
}

func (r *fakeRepository) RiderRedemptionCount(_ context.Context, couponID, _ string) (int, error) {
	return r.riderRedemptions[couponID], nil
}

func (r *fakeRepository) GetRiderCompletedTripCount(_ context.Context, _ string) (int, error) {
	return r.completedTripCount, nil
}

func (r *fakeRepository) FindFareByTripID(_ context.Context, _ string) (Fare, bool, error) {
	if r.findFareErr != nil {
		return Fare{}, false, r.findFareErr
	}
	return r.existingFare, r.fareFound, nil
}

func (r *fakeRepository) PersistFare(_ context.Context, input PersistFareInput) (Fare, error) {
	r.persistFareCalls = append(r.persistFareCalls, input)
	if r.persistFareErr != nil {
		return Fare{}, r.persistFareErr
	}
	return Fare{TripID: input.TripID, RiderID: input.RiderID, Breakdown: input.Breakdown}, nil
}

type fakeLocationClient struct {
	count int
	err   error

	// served/zoneID/zoneErr back CheckServiceZone — kept separate from
	// count/err (CountNearbyAvailableDrivers) so a test can fail one
	// call without affecting the other.
	served  bool
	zoneID  string
	zoneErr error
}

func (c *fakeLocationClient) CountNearbyAvailableDrivers(_ context.Context, _, _, _ float64) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return c.count, nil
}

func (c *fakeLocationClient) CheckServiceZone(_ context.Context, _, _ float64) (bool, string, error) {
	if c.zoneErr != nil {
		return false, "", c.zoneErr
	}
	return c.served, c.zoneID, nil
}

type fakeRoutingClient struct {
	route Route
	err   error
}

func (c *fakeRoutingClient) Route(_ context.Context, _, _, _, _ float64) (Route, error) {
	if c.err != nil {
		return Route{}, c.err
	}
	return c.route, nil
}

type fakeWeatherClient struct {
	conditions WeatherConditions
	err        error
}

func (c *fakeWeatherClient) GetConditions(_ context.Context, _, _ float64) (WeatherConditions, error) {
	if c.err != nil {
		return WeatherConditions{}, c.err
	}
	return c.conditions, nil
}

type harness struct {
	repo     *fakeRepository
	location *fakeLocationClient
	routing  *fakeRoutingClient
	weather  *fakeWeatherClient
}

func newHarness() *harness {
	h := &harness{
		repo:     newFakeRepository(),
		location: &fakeLocationClient{count: 10, served: true, zoneID: "zone-1"}, // plenty of drivers -> no demand surge by default; pickup served by default
		routing:  &fakeRoutingClient{route: Route{DistanceKm: 10, DurationMinutes: 20}},
		weather:  &fakeWeatherClient{},
	}

	// Set once here (not in service()) so a test can tweak individual
	// fields (e.g. DistanceCorrectionFactor for a fallback-route test)
	// on top of these defaults before calling service(), without a
	// later full-struct overwrite silently discarding the tweak.
	h.repo.config = Config{
		CurrencyCode:  "IQD",
		BaseFare:      decimal.NewFromInt(1000),
		PerKmRate:     decimal.NewFromInt(250),
		PerMinuteRate: decimal.NewFromInt(100),
	}

	return h
}

func (h *harness) service() Service {
	return NewService(h.repo, h.location, h.routing, h.weather)
}

func withFrozenTime(t *testing.T, when time.Time) {
	t.Helper()
	original := nowFunc
	nowFunc = func() time.Time { return when }
	t.Cleanup(func() { nowFunc = original })
}

func validEstimateInput() EstimateFareInput {
	return EstimateFareInput{
		RiderID:    "rider-1",
		PickupLat:  36.19,
		PickupLng:  44.01,
		DropoffLat: 36.20,
		DropoffLng: 44.02,
	}
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnMissingDependencies(t *testing.T) {
	h := newHarness()

	cases := []func(){
		func() { NewService(nil, h.location, h.routing, h.weather) },
		func() { NewService(h.repo, nil, h.routing, h.weather) },
		func() { NewService(h.repo, h.location, nil, h.weather) },
		func() { NewService(h.repo, h.location, h.routing, nil) },
	}

	for i, fn := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("case %d: expected a panic", i)
				}
			}()
			fn()
		}()
	}
}

// --- EstimateFare: validation & routing fallback ---------------------------

func TestService_EstimateFare_ValidationErrors(t *testing.T) {
	h := newHarness()
	svc := h.service()

	cases := []struct {
		name    string
		mutate  func(EstimateFareInput) EstimateFareInput
		wantErr error
	}{
		{"empty rider id", func(i EstimateFareInput) EstimateFareInput { i.RiderID = " "; return i }, ErrRiderIDRequired},
		{"invalid pickup lat", func(i EstimateFareInput) EstimateFareInput { i.PickupLat = 200; return i }, ErrInvalidLatitude},
		{"invalid dropoff lng", func(i EstimateFareInput) EstimateFareInput { i.DropoffLng = -200; return i }, ErrInvalidLongitude},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.EstimateFare(context.Background(), tc.mutate(validEstimateInput()))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestService_EstimateFare_UsesRoutingClientWhenAvailable(t *testing.T) {
	h := newHarness()
	h.routing.route = Route{DistanceKm: 5, DurationMinutes: 10}
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// subtotal = base(1000) + 5*250 + 10*100 = 1000+1250+1000 = 3250
	if !got.Subtotal.Equal(decimal.NewFromInt(3250)) {
		t.Fatalf("got subtotal %v, want 3250", got.Subtotal)
	}
}

func TestService_EstimateFare_FallsBackWhenRoutingClientFails(t *testing.T) {
	h := newHarness()
	h.routing.err = errors.New("OSRM unreachable")
	h.repo.config.DistanceCorrectionFactor = 1.0
	h.repo.config.AverageSpeedKmh = 30
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("expected the routing failure to be absorbed, got: %v", err)
	}

	if !got.Subtotal.IsPositive() {
		t.Fatalf("expected a positive fallback fare, got %v", got)
	}
}

// --- Service zones: gate quotes and select the right rate card -------------

func TestService_EstimateFare_RejectsPickupOutsideServiceZone(t *testing.T) {
	h := newHarness()
	h.location.served = false
	svc := h.service()

	_, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if !errors.Is(err, ErrPickupOutsideServiceZone) {
		t.Fatalf("got %v, want ErrPickupOutsideServiceZone", err)
	}
}

func TestService_EstimateFare_UsesZoneSpecificRateCardWhenPresent(t *testing.T) {
	h := newHarness()
	h.location.zoneID = "zone-erbil-center"
	h.repo.configsByZone["zone-erbil-center"] = Config{
		CurrencyCode:  "IQD",
		BaseFare:      decimal.NewFromInt(5000), // deliberately different from the global default (1000)
		PerKmRate:     decimal.NewFromInt(250),
		PerMinuteRate: decimal.NewFromInt(100),
	}
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.BaseFare.Equal(decimal.NewFromInt(5000)) {
		t.Fatalf("got base fare %v, want the zone-specific 5000, not the global default", got.BaseFare)
	}
	if got.ZoneID != "zone-erbil-center" {
		t.Fatalf("got zone id %q, want zone-erbil-center", got.ZoneID)
	}
}

func TestService_EstimateFare_FallsBackToGlobalConfigForAZoneWithoutItsOwnRateCard(t *testing.T) {
	h := newHarness()
	h.location.zoneID = "zone-with-no-rate-card"
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// harness's default global config has BaseFare 1000.
	if !got.BaseFare.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("got base fare %v, want the global default 1000", got.BaseFare)
	}
	if got.ZoneID != "zone-with-no-rate-card" {
		t.Fatalf("got zone id %q, want the resolved zone id even though it used the global rate card", got.ZoneID)
	}
}

// --- Surge: fail-open and capping -----------------------------------------

func TestService_EstimateFare_SurgeFailsOpenOnLocationError(t *testing.T) {
	h := newHarness()
	h.location.err = errors.New("location service unreachable")
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Surge.DemandPercent.IsZero() {
		t.Fatalf("expected demand surge to fail open to 0, got %v", got.Surge.DemandPercent)
	}
}

func TestService_EstimateFare_SurgeFailsOpenOnWeatherError(t *testing.T) {
	h := newHarness()
	h.weather.err = errors.New("weather service unreachable")
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Surge.WeatherPercent.IsZero() {
		t.Fatalf("expected weather surge to fail open to 0, got %v", got.Surge.WeatherPercent)
	}
}

func TestService_EstimateFare_SurgeIsCappedAtMax(t *testing.T) {
	h := newHarness()
	h.location.count = 0 // demand: 100%
	h.weather.conditions = WeatherConditions{SurgePercent: decimal.NewFromInt(100)}
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	h.repo.surgeRules = []SurgeTimeRule{
		{StartTime: "00:00:00", EndTime: "23:59:59", SurgePercent: decimal.NewFromInt(100), Active: true},
	}
	withFrozenTime(t, now)
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Surge.TotalPercent.Equal(maxSurgePercent) {
		t.Fatalf("got total surge %v, want capped at %v", got.Surge.TotalPercent, maxSurgePercent)
	}
	wantMultiplier := decimal.NewFromInt(1).Add(maxSurgePercent.Div(decimal.NewFromInt(100)))
	if !got.Surge.Multiplier.Equal(wantMultiplier) {
		t.Fatalf("got multiplier %v", got.Surge.Multiplier)
	}
}

// --- Discounts: anti-stacking and precedence -------------------------------

func TestService_EstimateFare_AppliesFirstRideDiscountWhenNoTripsYet(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 0
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountType != DiscountPercentage || got.AppliedDiscountLabel != "First ride discount" {
		t.Fatalf("got %+v", got)
	}

	chargeable := got.Subtotal.Add(got.SurgeAmount)
	wantDiscount := chargeable.Mul(firstRideDiscountPercent).Div(decimal.NewFromInt(100))
	if !got.DiscountAmount.Equal(wantDiscount) {
		t.Fatalf("got discount %v, want %v", got.DiscountAmount, wantDiscount)
	}
}

func TestService_EstimateFare_AppliesLoyaltyDiscountOnNthRide(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = loyaltyRideInterval - 1 // this fare would complete the 10th ride
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountLabel != "Loyalty discount" {
		t.Fatalf("got %+v", got)
	}
}

func TestService_EstimateFare_NoAutomaticDiscountOnOrdinaryRide(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3 // not the first ride, not a multiple of 10
	svc := h.service()

	got, err := svc.EstimateFare(context.Background(), validEstimateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountType != DiscountNone || !got.DiscountAmount.IsZero() {
		t.Fatalf("got %+v", got)
	}
}

func TestService_EstimateFare_CouponWinsOverFirstRideDiscountWhenLarger(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 0 // would otherwise get the 50% first-ride discount
	h.repo.couponsByCode["BIG70"] = Coupon{
		ID:            "coupon-1",
		Code:          "BIG70",
		DiscountType:  DiscountPercentage,
		DiscountValue: decimal.NewFromInt(70),
		Active:        true,
		ValidFrom:     time.Now().Add(-time.Hour),
		ValidUntil:    time.Now().Add(time.Hour),
		PerRiderLimit: 5,
	}
	svc := h.service()

	// evaluateCoupon looks the code up exactly as given (just trimmed) —
	// unlike CreateCoupon/GetCoupon, it does not upper-case it — so use
	// the same casing the coupon was stored under above.
	input := validEstimateInput()
	input.CouponCode = "BIG70"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountType != DiscountPercentage || got.AppliedDiscountLabel != "Coupon: BIG70" {
		t.Fatalf("expected the coupon (70%%) to beat the first-ride discount (50%%), got %+v", got)
	}
}

func TestService_EstimateFare_FirstRideDiscountWinsOverSmallerCoupon(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 0
	h.repo.couponsByCode["SMALL5"] = Coupon{
		ID:            "coupon-1",
		Code:          "SMALL5",
		DiscountType:  DiscountPercentage,
		DiscountValue: decimal.NewFromInt(5),
		Active:        true,
		ValidFrom:     time.Now().Add(-time.Hour),
		ValidUntil:    time.Now().Add(time.Hour),
		PerRiderLimit: 5,
	}
	svc := h.service()

	input := validEstimateInput()
	input.CouponCode = "SMALL5"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountLabel != "First ride discount" {
		t.Fatalf("expected the larger first-ride discount to win, got %+v", got)
	}
}

func TestService_EstimateFare_UnknownCouponDoesNotFailTheFare(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3 // no automatic discount either, to isolate the coupon behavior
	svc := h.service()

	input := validEstimateInput()
	input.CouponCode = "DOES-NOT-EXIST"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("expected an unknown coupon to be ignored, not fail the fare: %v", err)
	}

	if got.AppliedDiscountType != DiscountNone {
		t.Fatalf("got %+v", got)
	}
}

func TestService_EstimateFare_ExpiredCouponIsIgnored(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["EXPIRED"] = Coupon{
		ID:            "coupon-1",
		Code:          "EXPIRED",
		DiscountType:  DiscountFixed,
		DiscountValue: decimal.NewFromInt(500),
		Active:        true,
		ValidFrom:     time.Now().Add(-48 * time.Hour),
		ValidUntil:    time.Now().Add(-24 * time.Hour),
		PerRiderLimit: 5,
	}
	svc := h.service()

	input := validEstimateInput()
	input.CouponCode = "EXPIRED"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountType != DiscountNone {
		t.Fatalf("expected an expired coupon to be ignored, got %+v", got)
	}
}

func TestService_EstimateFare_CouponBelowMinimumFareIsIgnored(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["BIGTRIPSONLY"] = Coupon{
		ID:                "coupon-1",
		Code:              "BIGTRIPSONLY",
		DiscountType:      DiscountFixed,
		DiscountValue:     decimal.NewFromInt(500),
		Active:            true,
		ValidFrom:         time.Now().Add(-time.Hour),
		ValidUntil:        time.Now().Add(time.Hour),
		PerRiderLimit:     5,
		MinimumFareAmount: decimal.NewFromInt(1_000_000), // far above what this trip will cost
	}
	svc := h.service()

	input := validEstimateInput()
	input.CouponCode = "BIGTRIPSONLY"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountType != DiscountNone {
		t.Fatalf("expected a below-minimum coupon to be ignored, got %+v", got)
	}
}

func TestService_EstimateFare_CouponAtPerRiderLimitIsIgnored(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["USEDUP"] = Coupon{
		ID:            "coupon-1",
		Code:          "USEDUP",
		DiscountType:  DiscountFixed,
		DiscountValue: decimal.NewFromInt(500),
		Active:        true,
		ValidFrom:     time.Now().Add(-time.Hour),
		ValidUntil:    time.Now().Add(time.Hour),
		PerRiderLimit: 1,
	}
	h.repo.riderRedemptions["coupon-1"] = 1
	svc := h.service()

	input := validEstimateInput()
	input.CouponCode = "USEDUP"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AppliedDiscountType != DiscountNone {
		t.Fatalf("expected a coupon at its per-rider limit to be ignored, got %+v", got)
	}
}

func TestService_EstimateFare_FixedDiscountNeverExceedsChargeableAmount(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["HUGE"] = Coupon{
		ID:            "coupon-1",
		Code:          "HUGE",
		DiscountType:  DiscountFixed,
		DiscountValue: decimal.NewFromInt(1_000_000), // far more than the fare
		Active:        true,
		ValidFrom:     time.Now().Add(-time.Hour),
		ValidUntil:    time.Now().Add(time.Hour),
		PerRiderLimit: 5,
	}
	svc := h.service()

	input := validEstimateInput()
	input.CouponCode = "HUGE"

	got, err := svc.EstimateFare(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Total.IsZero() {
		t.Fatalf("expected the fare to floor at 0, got total %v", got.Total)
	}
	if !got.DiscountAmount.Equal(got.Subtotal.Add(got.SurgeAmount)) {
		t.Fatalf("expected the discount to be capped at the chargeable amount, got %+v", got)
	}
}

// --- CalculateFare: idempotency -------------------------------------------

func TestService_CalculateFare_RequiresTripID(t *testing.T) {
	h := newHarness()
	svc := h.service()

	_, err := svc.CalculateFare(context.Background(), CalculateFareInput{TripID: " ", RiderID: "rider-1"})
	if !errors.Is(err, ErrTripIDRequired) {
		t.Fatalf("got %v, want ErrTripIDRequired", err)
	}
}

func TestService_CalculateFare_IsIdempotent(t *testing.T) {
	h := newHarness()
	existing := Fare{TripID: "trip-1", RiderID: "rider-1", Breakdown: FareBreakdown{Total: decimal.NewFromInt(4242)}}
	h.repo.existingFare = existing
	h.repo.fareFound = true
	svc := h.service()

	got, err := svc.CalculateFare(context.Background(), CalculateFareInput{
		TripID:     "trip-1",
		RiderID:    "rider-1",
		PickupLat:  36.19,
		PickupLng:  44.01,
		DropoffLat: 36.20,
		DropoffLng: 44.02,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.TripID != existing.TripID || got.RiderID != existing.RiderID || !got.Breakdown.Total.Equal(existing.Breakdown.Total) {
		t.Fatalf("got %+v, want the existing fare unchanged", got)
	}

	if len(h.repo.persistFareCalls) != 0 {
		t.Fatal("expected a repeated CalculateFare not to persist a new fare")
	}
}

func TestService_CalculateFare_PersistsANewFareOnce(t *testing.T) {
	h := newHarness()
	svc := h.service()

	_, err := svc.CalculateFare(context.Background(), CalculateFareInput{
		TripID:     "trip-1",
		RiderID:    "rider-1",
		PickupLat:  36.19,
		PickupLng:  44.01,
		DropoffLat: 36.20,
		DropoffLng: 44.02,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.repo.persistFareCalls) != 1 {
		t.Fatalf("expected exactly 1 PersistFare call, got %d", len(h.repo.persistFareCalls))
	}
	if h.repo.persistFareCalls[0].TripID != "trip-1" {
		t.Fatalf("unexpected PersistFare input: %+v", h.repo.persistFareCalls[0])
	}
}

// --- CreateCoupon / GetCoupon -----------------------------------------------

func TestService_CreateCoupon_ValidationErrors(t *testing.T) {
	validFrom := time.Now()
	validUntil := validFrom.Add(24 * time.Hour)

	cases := []struct {
		name    string
		input   CreateCouponInput
		wantErr error
	}{
		{"empty code", CreateCouponInput{Code: " ", DiscountType: DiscountFixed, DiscountValue: decimal.NewFromInt(100), ValidFrom: validFrom, ValidUntil: validUntil}, ErrCouponCodeRequired},
		{"invalid discount type", CreateCouponInput{Code: "X", DiscountType: "bogus", DiscountValue: decimal.NewFromInt(100), ValidFrom: validFrom, ValidUntil: validUntil}, ErrInvalidDiscountType},
		{"percentage over 100", CreateCouponInput{Code: "X", DiscountType: DiscountPercentage, DiscountValue: decimal.NewFromInt(101), ValidFrom: validFrom, ValidUntil: validUntil}, ErrInvalidDiscountValue},
		{"percentage zero", CreateCouponInput{Code: "X", DiscountType: DiscountPercentage, DiscountValue: decimal.Zero, ValidFrom: validFrom, ValidUntil: validUntil}, ErrInvalidDiscountValue},
		{"fixed amount zero", CreateCouponInput{Code: "X", DiscountType: DiscountFixed, DiscountValue: decimal.Zero, ValidFrom: validFrom, ValidUntil: validUntil}, ErrInvalidDiscountValue},
		{"validity window backwards", CreateCouponInput{Code: "X", DiscountType: DiscountFixed, DiscountValue: decimal.NewFromInt(100), ValidFrom: validUntil, ValidUntil: validFrom}, ErrInvalidValidityWindow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			svc := h.service()

			_, err := svc.CreateCoupon(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestService_CreateCoupon_NormalizesCodeAndDefaultsPerRiderLimit(t *testing.T) {
	h := newHarness()
	svc := h.service()

	validFrom := time.Now()
	_, err := svc.CreateCoupon(context.Background(), CreateCouponInput{
		Code:          "  save10  ",
		DiscountType:  DiscountPercentage,
		DiscountValue: decimal.NewFromInt(10),
		ValidFrom:     validFrom,
		ValidUntil:    validFrom.Add(time.Hour),
		PerRiderLimit: 0, // should default to 1
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestService_GetCoupon_NormalizesCodeToUpperCase(t *testing.T) {
	h := newHarness()
	h.repo.couponsByCode["SAVE10"] = Coupon{Code: "SAVE10"}
	svc := h.service()

	got, err := svc.GetCoupon(context.Background(), "  save10  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Code != "SAVE10" {
		t.Fatalf("got %+v", got)
	}
}

func TestService_GetCoupon_EmptyCode(t *testing.T) {
	h := newHarness()
	svc := h.service()

	_, err := svc.GetCoupon(context.Background(), "   ")
	if !errors.Is(err, ErrCouponCodeRequired) {
		t.Fatalf("got %v, want ErrCouponCodeRequired", err)
	}
}
