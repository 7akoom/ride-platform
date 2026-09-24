package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// nowFunc is a package-level indirection so tests can freeze time (surge
// rules, coupon validity and quote expiry are all time-dependent, which is
// painful to test against the real clock).
var nowFunc = func() time.Time {
	return time.Now().UTC()
}

// supplyRadiusMeters is how far around the pickup free drivers count as
// supply, and how far a quote looks for the nearest driver of its class.
const supplyRadiusMeters = 5000

func validateCoordinates(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return ErrInvalidLatitude
	}

	if lng < -180 || lng > 180 {
		return ErrInvalidLongitude
	}

	return nil
}

type fareRequest struct {
	RiderID    string
	PickupLat  float64
	PickupLng  float64
	DropoffLat float64
	DropoffLng float64
	CouponCode string
}

func (r fareRequest) validate() (string, error) {
	riderID := strings.TrimSpace(r.RiderID)
	if riderID == "" {
		return "", ErrRiderIDRequired
	}

	if err := validateCoordinates(r.PickupLat, r.PickupLng); err != nil {
		return "", err
	}

	if err := validateCoordinates(r.DropoffLat, r.DropoffLng); err != nil {
		return "", err
	}

	return riderID, nil
}

// market is everything about a request that does not depend on the vehicle
// class, gathered once whether one class is priced or all of them.
type market struct {
	request fareRequest
	riderID string
	zone    ServiceZone
	now     time.Time

	// route is the OSRM route from pickup to dropoff; when routeOK is false
	// each class falls back to its own card's straight-line estimate.
	route   Route
	routeOK bool

	// drivers are the free drivers near the pickup, nearest first; unknown
	// (driversKnown false) when they could not be looked up.
	drivers      []NearbyDriver
	driversKnown bool

	// otherRiders is how many other riders asked for a quote in the zone
	// during the demand window.
	otherRiders int

	// The staff-set surge that applies: the larger of the hour's rule and
	// the zone's surge.
	timePercent decimal.Decimal
	zonePercent decimal.Decimal
	staffLabel  string

	weatherPercent decimal.Decimal

	// promotions is what the rider's discounts depend on.
	promotions promotions
}

// gatherMarket checks the pickup is served, then reads what the price
// depends on at the same time: the route, the free drivers nearby, the
// weather, the demand and the staff surge. Only a failure to read this
// service's own data fails the request; the route, the drivers and the
// weather fail open.
func (s *service) gatherMarket(ctx context.Context, request fareRequest, riderID string) (market, error) {
	zone, err := s.locationClient.CheckServiceZone(ctx, request.PickupLat, request.PickupLng)
	if err != nil {
		return market{}, fmt.Errorf("check service zone: %w", err)
	}

	if !zone.Served {
		return market{}, ErrPickupOutsideServiceZone
	}

	m := market{request: request, riderID: riderID, zone: zone, now: nowFunc()}

	var (
		wg                                          sync.WaitGroup
		rulesErr, zoneSurgeErr, demandErr, promoErr error
		rules                                       []SurgeTimeRule
		zoneSurge                                   ZoneSurge
		zoneSurgeFound                              bool
	)

	wg.Add(7)

	go func() {
		defer wg.Done()

		m.promotions, promoErr = s.readPromotions(ctx, riderID, request.CouponCode)
	}()

	go func() {
		defer wg.Done()

		route, err := s.routingClient.Route(ctx, request.PickupLat, request.PickupLng, request.DropoffLat, request.DropoffLng)
		if err == nil {
			m.route, m.routeOK = route, true
		}
	}()

	go func() {
		defer wg.Done()

		drivers, err := s.driverFinder.AvailableDriversNear(ctx, request.PickupLat, request.PickupLng, supplyRadiusMeters)
		if err == nil {
			m.drivers, m.driversKnown = drivers, true
		}
	}()

	go func() {
		defer wg.Done()

		m.weatherPercent = s.weatherSurgePercent(ctx, request.PickupLat, request.PickupLng)
	}()

	go func() {
		defer wg.Done()

		m.otherRiders, demandErr = s.repository.CountQuotingRiders(ctx, zone.ZoneID, riderID, m.now.Add(-demandWindow))
	}()

	go func() {
		defer wg.Done()

		rules, rulesErr = s.repository.ListActiveSurgeTimeRules(ctx)
	}()

	go func() {
		defer wg.Done()

		zoneSurge, zoneSurgeFound, zoneSurgeErr = s.repository.ActiveZoneSurge(ctx, zone.ZoneID, m.now)
	}()

	wg.Wait()

	if err := errors.Join(rulesErr, zoneSurgeErr, demandErr); err != nil {
		return market{}, fmt.Errorf("read surge inputs: %w", err)
	}

	if promoErr != nil {
		return market{}, promoErr
	}

	m.timePercent, m.staffLabel = timeRuleSurge(rulesFor(rules, zone), localTime(m.now, zone.TimeZone))

	if zoneSurgeFound && zoneSurge.SurgePercent.GreaterThan(m.timePercent) {
		m.zonePercent = zoneSurge.SurgePercent
		m.staffLabel = zoneSurge.Reason
	} else if zoneSurgeFound {
		m.zonePercent = zoneSurge.SurgePercent
	}

	return m, nil
}

// routeFor is the market's OSRM route, or when OSRM could not be reached a
// straight-line estimate with the card's correction and speed. Fares are the
// one thing a rider always needs an answer for, so an unreachable routing
// engine turns into a slightly less accurate price, flagged Estimated.
func (m market) routeFor(config Config) Route {
	if m.routeOK {
		return m.route
	}

	return fallbackRoute(config, m.request.PickupLat, m.request.PickupLng, m.request.DropoffLat, m.request.DropoffLng)
}

// pricedClass is one class's price and what it came from.
type pricedClass struct {
	breakdown FareBreakdown
	coupon    *AppliedCoupon
	config    Config
}

// priceClass is the one place the pricing pipeline lives, shared by
// EstimateFare, CalculateFare and QuoteTrip so an estimate or a quote can
// never drift from what the rider is actually charged: rate card ->
// distance/duration -> minimum fare -> surge -> discount -> total.
func (s *service) priceClass(ctx context.Context, m market, vehicleClass string) (pricedClass, error) {
	config, err := s.repository.GetActiveConfig(ctx, Scope{
		ZoneID:       m.zone.ZoneID,
		CityID:       m.zone.CityID,
		VehicleClass: vehicleClass,
	})
	if err != nil {
		return pricedClass{}, fmt.Errorf("get active pricing config: %w", err)
	}

	breakdown := baseFareBreakdown(config, m.routeFor(config))
	breakdown.ZoneID = m.zone.ZoneID
	breakdown.CityID = m.zone.CityID
	breakdown.VehicleClass = vehicleClass

	breakdown.Surge = surgeFor(config, m)
	breakdown.SurgeAmount = breakdown.Subtotal.Mul(breakdown.Surge.Multiplier.Sub(decimal.NewFromInt(1)))

	// Discounts apply to the surged amount, not the pre-surge subtotal —
	// otherwise a percentage coupon would be worth less exactly when the
	// rider is paying the most.
	chargeable := breakdown.Subtotal.Add(breakdown.SurgeAmount)

	discount := selectBestDiscount(m.promotions, m.zone, vehicleClass, chargeable, m.now)

	breakdown.AppliedDiscountType = discount.Type
	breakdown.AppliedDiscountLabel = discount.Label
	breakdown.DiscountAmount = discount.Amount
	breakdown.CouponStatus = discount.CouponStatus

	total := chargeable.Sub(discount.Amount)
	if total.IsNegative() {
		total = decimal.Zero
	}

	// The one and only place a fare is rounded: what the rider is quoted,
	// what is stored, what fare.calculated publishes and what wallet-service
	// settles all come from this single value.
	breakdown.Total = roundToIncrement(total, s.fareRoundingIncrement)

	return pricedClass{breakdown: breakdown, coupon: discount.Coupon, config: config}, nil
}

// buildFare prices one class of one request from scratch.
func (s *service) buildFare(ctx context.Context, request fareRequest, rawClass string) (pricedClass, error) {
	riderID, err := request.validate()
	if err != nil {
		return pricedClass{}, err
	}

	vehicleClass, err := NormalizeVehicleClass(rawClass)
	if err != nil {
		return pricedClass{}, err
	}

	m, err := s.gatherMarket(ctx, request, riderID)
	if err != nil {
		return pricedClass{}, err
	}

	return s.priceClass(ctx, m, vehicleClass)
}

// locations caches loaded time zones; a city's zone is read on every fare.
var locations sync.Map

// localTime is now in the named IANA time zone, or in UTC when the name is
// empty or unknown (a city's zone is checked when it is saved, so that
// only happens for data written before cities had one).
func localTime(now time.Time, timeZone string) time.Time {
	if timeZone == "" {
		return now.UTC()
	}

	if cached, ok := locations.Load(timeZone); ok {
		return now.In(cached.(*time.Location))
	}

	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return now.UTC()
	}

	locations.Store(timeZone, location)

	return now.In(location)
}
