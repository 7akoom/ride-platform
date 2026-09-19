package pricing

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type CreateCouponInput struct {
	Code              string
	DiscountType      DiscountType
	DiscountValue     decimal.Decimal
	ValidFrom         time.Time
	ValidUntil        time.Time
	MaxRedemptions    *int
	PerRiderLimit     int
	MinimumFareAmount decimal.Decimal
}

// AppliedCoupon carries what CalculateFare needs to record a redemption,
// if a coupon was actually used.
type AppliedCoupon struct {
	CouponID       string
	DiscountAmount decimal.Decimal
}

type PersistFareInput struct {
	TripID    string
	RiderID   string
	Breakdown FareBreakdown
	Coupon    *AppliedCoupon // nil if no coupon was applied
}

// Repository is the persistence port. It intentionally groups config,
// surge rules, coupons, rider stats, and fares behind one interface —
// they're all in the same Postgres database and PersistFare needs to
// touch several of them atomically in a single transaction (fare row +
// rider stats increment + coupon redemption), the same "one repository,
// one transaction boundary" reasoning trip-service uses.
type Repository interface {
	// GetActiveConfig returns the newest config row for zoneID if one
	// exists, otherwise the newest global-default row (zone_id NULL).
	// zoneID is always a real zone id here — callers only reach this
	// after CheckServiceZone has confirmed the pickup is served.
	// vehicleClass narrows the lookup further: a rate card for that class wins
	// over one that applies to any class. Order, most specific first: (zone,
	// class), (zone, any class), (no zone, class), the global default.
	GetActiveConfig(ctx context.Context, zoneID, vehicleClass string) (Config, error)

	ListActiveSurgeTimeRules(ctx context.Context) ([]SurgeTimeRule, error)

	FindCouponByCode(ctx context.Context, code string) (Coupon, error)

	CreateCoupon(ctx context.Context, input CreateCouponInput) (Coupon, error)

	// RiderRedemptionCount is how many times this rider has already used
	// this specific coupon — checked against the coupon's per-rider limit.
	RiderRedemptionCount(ctx context.Context, couponID, riderID string) (int, error)

	// GetRiderCompletedTripCount reads the self-contained counter (see
	// migration 00005) — used for first-ride/loyalty discount checks.
	// Does NOT increment it; PersistFare does that as part of its
	// transaction, exactly once per real completed trip.
	GetRiderCompletedTripCount(ctx context.Context, riderID string) (int, error)

	// FindFareByTripID supports CalculateFare's idempotency: if a fare
	// already exists for this trip, return it instead of recalculating.
	FindFareByTripID(ctx context.Context, tripID string) (Fare, bool, error)

	PersistFare(ctx context.Context, input PersistFareInput) (Fare, error)
}

// LocationClient is pricing-service's view of location-service — used
// only to count nearby available drivers for the demand-based surge
// component (see the demand-surge design note in service_surge.go).
type LocationClient interface {
	CountNearbyAvailableDrivers(
		ctx context.Context,
		latitude, longitude, radiusMeters float64,
	) (int, error)

	// CheckServiceZone reports whether a point falls inside any active
	// service zone and, if so, which one — the same check trip-service
	// runs before accepting a trip request, reused here to pick the
	// right rate card and to refuse a quote for a location that could
	// never become a real trip.
	CheckServiceZone(
		ctx context.Context,
		latitude, longitude float64,
	) (served bool, zoneID string, err error)
}

// RoutingClient is a thin abstraction over OSRM (self-hosted,
// open-source, no API keys or usage limits). Returns real road-network
// distance and duration. A failure here must never fail fare
// calculation — the service falls back to a Haversine estimate, see
// routeOrFallback in service_fare.go.
type RoutingClient interface {
	Route(
		ctx context.Context,
		pickupLat, pickupLng, dropoffLat, dropoffLng float64,
	) (Route, error)
}

// WeatherConditions is deliberately minimal — just enough to drive the
// weather-surge tiers, not a general-purpose weather model. SurgePercent
// is decimal since it feeds directly into fare math (see
// service_surge.go's weatherSurgePercent).
type WeatherConditions struct {
	SurgePercent decimal.Decimal
}

// WeatherClient is a thin abstraction over Open-Meteo (free, no API key,
// no cost — see README for why this doesn't violate the
// no-paid-services constraint despite being an external call). A
// failure here must never fail fare calculation — see
// service_surge.go's fail-open handling.
type WeatherClient interface {
	GetConditions(
		ctx context.Context,
		latitude, longitude float64,
	) (WeatherConditions, error)
}
