package pricing

// Internal test package: needs to override the package-level nowFunc to
// freeze time for coupon-validity and surge tests.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	config    Config
	configErr error

	// lastVehicleClass is the class GetActiveConfig was last asked for, and
	// lastScope the whole scope.
	lastVehicleClass string
	lastScope        Scope

	// configsByZone / configsByCity let a test give one zone or city its
	// own rate card; GetActiveConfig falls back to config (the global
	// default) otherwise — mirrors the real repository's zone, then city,
	// then everywhere lookup.
	configsByZone map[string]Config
	configsByCity map[string]Config

	surgeRules []SurgeTimeRule

	zoneSurge      ZoneSurge
	zoneSurgeFound bool

	quotingRiders int

	couponsByCode map[string]Coupon

	riderRedemptions map[string]int // couponID -> count

	// promotionSettings nil means the defaults.
	promotionSettings *PromotionSettings

	// reservedCoupons is the coupon each trip holds (ClaimQuote reserves
	// it), and releasedTrips the trips ReleaseTripCoupon freed.
	reservedCoupons map[string]string
	releasedTrips   []string

	completedTripCount int

	existingFare Fare
	fareFound    bool
	findFareErr  error

	persistFareCalls []PersistFareInput
	persistFareErr   error
	// couponUnavailable makes PersistFare refuse a fare with a coupon, as
	// the repository does when no use of it is left.
	couponUnavailable bool

	quotes     map[string]Quote
	savedCount int
	claimErr   error
	released   []string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		couponsByCode:    map[string]Coupon{},
		riderRedemptions: map[string]int{},
		configsByZone:    map[string]Config{},
		configsByCity:    map[string]Config{},
		quotes:           map[string]Quote{},
		reservedCoupons:  map[string]string{},
	}
}

func (r *fakeRepository) GetActiveConfig(_ context.Context, scope Scope) (Config, error) {
	r.lastVehicleClass = scope.VehicleClass
	r.lastScope = scope

	if r.configErr != nil {
		return Config{}, r.configErr
	}

	if cfg, ok := r.configsByZone[scope.ZoneID]; ok && scope.ZoneID != "" {
		return cfg, nil
	}

	if cfg, ok := r.configsByCity[scope.CityID]; ok && scope.CityID != "" {
		return cfg, nil
	}

	return r.config, nil
}

func (r *fakeRepository) GetConfigByID(_ context.Context, id string) (Config, error) {
	if r.config.ID == id {
		return r.config, nil
	}

	for _, cards := range []map[string]Config{r.configsByZone, r.configsByCity} {
		for _, card := range cards {
			if card.ID == id {
				return card, nil
			}
		}
	}

	return Config{}, ErrNoActiveConfig
}

func (r *fakeRepository) ListActiveSurgeTimeRules(_ context.Context) ([]SurgeTimeRule, error) {
	return r.surgeRules, nil
}

func (r *fakeRepository) ActiveZoneSurge(_ context.Context, _ string, _ time.Time) (ZoneSurge, bool, error) {
	return r.zoneSurge, r.zoneSurgeFound, nil
}

func (r *fakeRepository) CountQuotingRiders(_ context.Context, _, _ string, _ time.Time) (int, error) {
	return r.quotingRiders, nil
}

func (r *fakeRepository) FindCouponByCode(_ context.Context, code string) (Coupon, error) {
	if c, ok := r.couponsByCode[code]; ok {
		return c, nil
	}
	return Coupon{}, ErrCouponNotFound
}

func (r *fakeRepository) FindCouponByID(_ context.Context, id string) (Coupon, error) {
	for _, c := range r.couponsByCode {
		if c.ID == id {
			return c, nil
		}
	}
	return Coupon{}, ErrCouponNotFound
}

func (r *fakeRepository) RiderRedemptionCount(_ context.Context, couponID, _ string) (int, error) {
	return r.riderRedemptions[couponID], nil
}

func (r *fakeRepository) GetPromotionSettings(_ context.Context) (PromotionSettings, error) {
	if r.promotionSettings != nil {
		return *r.promotionSettings, nil
	}
	return DefaultPromotionSettings(), nil
}

// releaseCoupon mirrors the repository: a trip's reserved use is freed.
func (r *fakeRepository) releaseCoupon(tripID string) {
	if couponID, ok := r.reservedCoupons[tripID]; ok {
		delete(r.reservedCoupons, tripID)
		r.riderRedemptions[couponID]--
	}
}

func (r *fakeRepository) ReleaseTripCoupon(_ context.Context, tripID string) error {
	r.releasedTrips = append(r.releasedTrips, tripID)
	r.releaseCoupon(tripID)
	return nil
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
	if r.couponUnavailable && input.Coupon != nil {
		return Fare{}, ErrCouponUnavailable
	}
	if r.persistFareErr != nil {
		return Fare{}, r.persistFareErr
	}
	return Fare{TripID: input.TripID, RiderID: input.RiderID, Kind: input.Kind, Breakdown: input.Breakdown, QuoteID: input.QuoteID}, nil
}

func (r *fakeRepository) SaveQuotes(_ context.Context, quotes []Quote) ([]Quote, error) {
	out := make([]Quote, 0, len(quotes))
	for _, q := range quotes {
		r.savedCount++
		q.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", r.savedCount)
		r.quotes[q.ID] = q
		out = append(out, q)
	}
	return out, nil
}

func (r *fakeRepository) FindQuote(_ context.Context, id string) (Quote, error) {
	q, ok := r.quotes[id]
	if !ok {
		return Quote{}, ErrQuoteNotFound
	}
	return q, nil
}

func (r *fakeRepository) ClaimQuote(_ context.Context, id, riderID, tripID string, now time.Time) (Quote, error) {
	if r.claimErr != nil {
		return Quote{}, r.claimErr
	}
	q, ok := r.quotes[id]
	switch {
	case !ok || q.RiderID != riderID:
		return Quote{}, ErrQuoteNotFound
	case q.ClaimedTripID == tripID:
		return q, nil
	case q.ClaimedTripID != "":
		return Quote{}, ErrQuoteAlreadyUsed
	case !now.Before(q.ExpiresAt):
		return Quote{}, ErrQuoteExpired
	}
	// Mirrors the repository: the coupon must still be on offer with a use
	// left for the rider, and one use is reserved for the trip.
	if q.Coupon != nil {
		coupon, found := Coupon{}, false
		for _, c := range r.couponsByCode {
			if c.ID == q.Coupon.CouponID {
				coupon, found = c, true
			}
		}
		if !found || !coupon.IsCurrentlyValid(now) || r.riderRedemptions[coupon.ID] >= coupon.PerRiderLimit {
			return Quote{}, ErrQuoteCouponUnavailable
		}
		r.riderRedemptions[coupon.ID]++
		r.reservedCoupons[tripID] = coupon.ID
	}
	q.ClaimedTripID = tripID
	r.quotes[id] = q
	return q, nil
}

func (r *fakeRepository) ReleaseQuote(_ context.Context, id, tripID string) error {
	r.released = append(r.released, id+"/"+tripID)
	if q, ok := r.quotes[id]; ok && q.ClaimedTripID == tripID {
		q.ClaimedTripID = ""
		r.quotes[id] = q
		r.releaseCoupon(tripID)
	}
	return nil
}

func (r *fakeRepository) DeleteUnclaimedQuotes(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

type fakeLocationClient struct {
	served   bool
	zoneID   string
	cityID   string
	timeZone string
	zoneErr  error
}

func (c *fakeLocationClient) CheckServiceZone(_ context.Context, _, _ float64) (ServiceZone, error) {
	if c.zoneErr != nil {
		return ServiceZone{}, c.zoneErr
	}
	return ServiceZone{Served: c.served, ZoneID: c.zoneID, CityID: c.cityID, TimeZone: c.timeZone}, nil
}

type fakeDriverFinder struct {
	drivers []NearbyDriver
	err     error
}

func (f *fakeDriverFinder) AvailableDriversNear(_ context.Context, _, _, _ float64) ([]NearbyDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.drivers, nil
}

// someDrivers is n free economy drivers about a kilometre from the pickup.
func someDrivers(n int) []NearbyDriver {
	drivers := make([]NearbyDriver, n)
	for i := range drivers {
		drivers[i] = NearbyDriver{
			DriverID:     fmt.Sprintf("driver-%d", i),
			VehicleClass: VehicleClassEconomy,
			Location:     Point{Latitude: 36.2, Longitude: 44.01},
		}
	}
	return drivers
}

type fakeRoutingClient struct {
	route Route
	err   error
	via   []Point
}

func (c *fakeRoutingClient) Route(_ context.Context, _, _, _, _ float64, via ...Point) (Route, error) {
	if len(via) > 0 {
		c.via = via
	}
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
	drivers  *fakeDriverFinder
}

func newHarness() *harness {
	h := &harness{
		repo:     newFakeRepository(),
		location: &fakeLocationClient{served: true, zoneID: "zone-1", cityID: "city-1"}, // pickup served by default
		routing:  &fakeRoutingClient{route: Route{DistanceKm: 10, DurationMinutes: 20}},
		weather:  &fakeWeatherClient{},
		drivers:  &fakeDriverFinder{drivers: someDrivers(10)}, // plenty of drivers -> no demand surge by default
	}

	// Set once here (not in service()) so a test can tweak individual
	// fields (e.g. DistanceCorrectionFactor for a fallback-route test)
	// on top of these defaults before calling service(), without a
	// later full-struct overwrite silently discarding the tweak.
	h.repo.config = Config{
		ID:              "config-global",
		CurrencyCode:    "IQD",
		BaseFare:        decimal.NewFromInt(1000),
		PerKmRate:       decimal.NewFromInt(250),
		PerMinuteRate:   decimal.NewFromInt(100),
		MaxSurgePercent: decimal.NewFromInt(150),
		DemandSurge:     true,
		WeatherSurge:    true,
	}

	return h
}

func (h *harness) service(options ...Option) Service {
	return NewService(h.repo, h.location, h.routing, h.weather, h.drivers, options...)
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
		func() { NewService(nil, h.location, h.routing, h.weather, h.drivers) },
		func() { NewService(h.repo, nil, h.routing, h.weather, h.drivers) },
		func() { NewService(h.repo, h.location, nil, h.weather, h.drivers) },
		func() { NewService(h.repo, h.location, h.routing, nil, h.drivers) },
		func() { NewService(h.repo, h.location, h.routing, h.weather, nil) },
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

func TestService_EstimateFare_SurgeFailsOpenWhenDriversCannotBeLookedUp(t *testing.T) {
	h := newHarness()
	h.drivers.err = errors.New("driver service unreachable")
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
	h.drivers.drivers = nil // no free driver: demand 50%
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

	maxSurgePercent := h.repo.config.MaxSurgePercent
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
	wantDiscount := chargeable.Mul(DefaultPromotionSettings().FirstRidePercent).Div(decimal.NewFromInt(100))
	if !got.DiscountAmount.Equal(wantDiscount) {
		t.Fatalf("got discount %v, want %v", got.DiscountAmount, wantDiscount)
	}
}

func TestService_EstimateFare_AppliesLoyaltyDiscountOnNthRide(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = DefaultPromotionSettings().LoyaltyEvery - 1 // this fare would complete the 10th ride
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

	// Codes are stored upper-case; the rider's code is upper-cased.
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
