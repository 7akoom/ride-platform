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
	TripID  string
	RiderID string
	// Kind is FareKindTrip unless it is a cancelled trip's fee: a fee does
	// not count as a completed trip nor use a coupon.
	Kind      FareKind
	Breakdown FareBreakdown
	Coupon    *AppliedCoupon // nil if no coupon was applied
	// QuoteID is the quote the fare came from (empty when priced on
	// completion); ConfigID the rate card used.
	QuoteID  string
	ConfigID string
}

// Repository is the persistence port. It intentionally groups config,
// surge rules, coupons, rider stats, quotes and fares behind one interface —
// they're all in the same Postgres database and PersistFare needs to
// touch several of them atomically in a single transaction (fare row +
// rider stats increment + coupon redemption), the same "one repository,
// one transaction boundary" reasoning trip-service uses.
type Repository interface {
	// GetActiveConfig returns the rate card in force for a pickup in the
	// scope's zone and city, for its vehicle class. Most specific first:
	// the zone's card, the city's, the one for everywhere; within each, the
	// class's own card before the one for every class. A retired newest
	// version takes its place out of the running.
	GetActiveConfig(ctx context.Context, scope Scope) (Config, error)

	// GetConfigByID returns one rate card version (a quote's), retired or
	// not; ErrNoActiveConfig when there is none with that id.
	GetConfigByID(ctx context.Context, configID string) (Config, error)

	// ListActiveSurgeTimeRules returns every active rule, wherever it
	// applies; the service keeps the ones for the pickup's zone and city.
	ListActiveSurgeTimeRules(ctx context.Context) ([]SurgeTimeRule, error)

	// ActiveZoneSurge returns the highest surge staff put on the zone that
	// is running at the given moment, if any.
	ActiveZoneSurge(ctx context.Context, zoneID string, at time.Time) (ZoneSurge, bool, error)

	// CountQuotingRiders counts the other riders who asked for a quote in
	// the zone since the given moment: the demand side of demand surge.
	CountQuotingRiders(ctx context.Context, zoneID, excludeRiderID string, since time.Time) (int, error)

	FindCouponByCode(ctx context.Context, code string) (Coupon, error)

	FindCouponByID(ctx context.Context, couponID string) (Coupon, error)

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

	// PersistFare returns ErrFareAlreadyRecorded when the trip already has a
	// fare (a concurrent call recorded it first).
	PersistFare(ctx context.Context, input PersistFareInput) (Fare, error)

	// SaveQuotes stores the quotes of one QuoteTrip call and returns them
	// with their ids.
	SaveQuotes(ctx context.Context, quotes []Quote) ([]Quote, error)

	// FindQuote returns ErrQuoteNotFound for an unknown id.
	FindQuote(ctx context.Context, quoteID string) (Quote, error)

	// ClaimQuote gives the quote to the trip if it is the rider's, has not
	// expired at now and no other trip has it; claiming it again for the same
	// trip returns it. Errors: ErrQuoteNotFound (unknown, or another
	// rider's), ErrQuoteExpired, ErrQuoteAlreadyUsed.
	ClaimQuote(ctx context.Context, quoteID, riderID, tripID string, now time.Time) (Quote, error)

	// ReleaseQuote frees the quote if that trip holds it; otherwise nothing.
	ReleaseQuote(ctx context.Context, quoteID, tripID string) error

	// DeleteUnclaimedQuotes removes quotes no trip claimed that expired
	// before the given moment, and returns how many.
	DeleteUnclaimedQuotes(ctx context.Context, expiredBefore time.Time) (int, error)
}

// ServiceZone is where a point is served: its zone, the zone's city and the
// city's IANA time zone.
type ServiceZone struct {
	Served   bool
	ZoneID   string
	CityID   string
	TimeZone string
}

// LocationClient is pricing-service's view of location-service.
type LocationClient interface {
	// CheckServiceZone reports whether a point falls inside any active
	// service zone and, if so, which one — the same check trip-service
	// runs before accepting a trip request, reused here to pick the
	// right rate card and surge rules and to refuse a quote for a location
	// that could never become a real trip.
	CheckServiceZone(ctx context.Context, latitude, longitude float64) (ServiceZone, error)
}

// NearbyDriver is a driver who could take a trip right now: active,
// available, and seen near the pickup in the last few seconds.
type NearbyDriver struct {
	DriverID       string
	VehicleClass   string
	Location       Point
	DistanceMeters float64
}

// DriverFinder finds the free drivers near a point, nearest first. It is
// the supply side of demand surge and gives each quote how far the nearest
// driver of its class is.
type DriverFinder interface {
	AvailableDriversNear(ctx context.Context, latitude, longitude, radiusMeters float64) ([]NearbyDriver, error)
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
