package pricing

import (
	"time"

	"github.com/shopspring/decimal"
)

type DiscountType string

const (
	DiscountNone       DiscountType = ""
	DiscountPercentage DiscountType = "percentage"
	DiscountFixed      DiscountType = "fixed_amount"
)

// Config is a rate card. One deployment = one currency (matches the "sell
// a separate instance per client" business model — no need for
// multi-currency inside one instance).
//
// Cards are versioned: setting one inserts a new row and the newest row for
// a place and class is the one in force, so the numbers past fares were
// priced with stay on file. A place is a zone, a city, or everywhere (both
// empty); the card for every class everywhere always exists.
type Config struct {
	ID string
	// At most one of ZoneID and CityID is set; neither means everywhere.
	ZoneID string
	CityID string
	// Empty means this rate card applies to any vehicle class; otherwise it
	// is specific to that class (economy, comfort).
	VehicleClass  string
	CurrencyCode  string
	BaseFare      decimal.Decimal
	PerKmRate     decimal.Decimal
	PerMinuteRate decimal.Decimal
	// MinimumFare is the least base + distance + duration comes to.
	MinimumFare decimal.Decimal

	// Waiting at the pickup, cancelling and not showing up (charged by the
	// trip lifecycle; kept on the card so staff set every price in one
	// place).
	FreeWaitingMinutes       int
	WaitingPerMinute         decimal.Decimal
	CancellationFee          decimal.Decimal
	CancellationGraceMinutes int
	NoShowFee                decimal.Decimal

	// MaxSurgePercent caps the surge (0 turns it off). DemandSurge and
	// WeatherSurge say whether those two sources count at all.
	MaxSurgePercent decimal.Decimal
	DemandSurge     bool
	WeatherSurge    bool

	AverageSpeedKmh          float64
	DistanceCorrectionFactor float64

	// Retired marks a version that takes the place's card away, so trips
	// there fall back to the next card.
	Retired bool
	// CreatedBy is the staff member's identity id; empty for seeded cards.
	CreatedBy string
	CreatedAt time.Time
}

// Scope says where a rate card or surge rule applies.
type Scope struct {
	ZoneID       string
	CityID       string
	VehicleClass string
}

// SurgeTimeRule is one entry in the configurable peak-hours schedule.
// DayOfWeek is nil for "every day" (matches Postgres EXTRACT(DOW...):
// 0=Sunday..6=Saturday). The day and the times are the local ones of the
// city the pickup is in. A rule applies everywhere, in one city, or in one
// zone.
type SurgeTimeRule struct {
	ID           string
	Label        string
	ZoneID       string
	CityID       string
	DayOfWeek    *int
	StartTime    string // "HH:MM:SS", kept as string — no calendar date involved
	EndTime      string
	SurgePercent decimal.Decimal
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ZoneSurge is a surge staff put on one zone for a while.
type ZoneSurge struct {
	ID           string
	ZoneID       string
	SurgePercent decimal.Decimal
	Reason       string
	StartsAt     time.Time
	EndsAt       time.Time
	EndedAt      *time.Time
	CreatedBy    string
	CreatedAt    time.Time
}

// SurgeBreakdown lets each contributing factor stay visible instead of
// collapsing straight to a multiplier. Kept as decimal, not float64,
// same as every other value that feeds directly into money math below
// (see FareBreakdown) — a percentage that multiplies a fare is exactly
// as precision-sensitive as the fare itself.
//
// TotalPercent is the larger of TimeOfDayPercent and ZonePercent (both set
// by staff, never added up) plus DemandPercent and WeatherPercent, capped by
// the rate card.
type SurgeBreakdown struct {
	TimeOfDayPercent decimal.Decimal
	ZonePercent      decimal.Decimal
	DemandPercent    decimal.Decimal
	WeatherPercent   decimal.Decimal
	TotalPercent     decimal.Decimal
	Multiplier       decimal.Decimal
	// Label is what staff called the rule or zone surge that applies.
	Label string
}

type Coupon struct {
	ID            string
	Code          string
	Description   string
	DiscountType  DiscountType
	DiscountValue decimal.Decimal
	// MaxDiscountAmount caps a percentage's discount; nil means no cap.
	MaxDiscountAmount *decimal.Decimal
	ValidFrom         time.Time
	ValidUntil        time.Time
	// MaxRedemptions nil means unlimited. RedemptionCount is the uses held:
	// redeemed by completed trips and reserved by trips under way.
	MaxRedemptions    *int
	RedemptionCount   int
	PerRiderLimit     int
	MinimumFareAmount decimal.Decimal
	// At most one of CityID and ZoneID: where the pickup must be. Empty
	// VehicleClasses means every class.
	CityID         string
	ZoneID         string
	VehicleClasses []string
	// NewRidersOnly: only riders who never completed a trip.
	NewRidersOnly bool
	Active        bool
	CreatedBy     string
	UpdatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Coupon states, as staff see them.
const (
	CouponStateRunning   = "running"
	CouponStateScheduled = "scheduled"
	CouponStateExpired   = "expired"
	CouponStateUsedUp    = "used_up"
	CouponStateEnded     = "ended"
)

// State is where the coupon stands at now: turned off, not started, over,
// every use taken, or running.
func (c Coupon) State(now time.Time) string {
	switch {
	case !c.Active:
		return CouponStateEnded
	case now.After(c.ValidUntil):
		return CouponStateExpired
	case c.MaxRedemptions != nil && c.RedemptionCount >= *c.MaxRedemptions:
		return CouponStateUsedUp
	case now.Before(c.ValidFrom):
		return CouponStateScheduled
	default:
		return CouponStateRunning
	}
}

// IsCurrentlyValid checks time window and total-redemption cap only —
// per-rider usage is checked separately since it needs a DB lookup.
func (c Coupon) IsCurrentlyValid(now time.Time) bool {
	return c.State(now) == CouponStateRunning
}

// AppliesToClass reports whether the coupon may be used for the class.
func (c Coupon) AppliesToClass(class string) bool {
	if len(c.VehicleClasses) == 0 {
		return true
	}

	for _, allowed := range c.VehicleClasses {
		if allowed == class {
			return true
		}
	}

	return false
}

// CouponStatus says what became of the code a rider entered.
type CouponStatus string

const (
	CouponStatusNone           CouponStatus = ""
	CouponStatusApplied        CouponStatus = "applied"
	CouponStatusNotFound       CouponStatus = "not_found"
	CouponStatusEnded          CouponStatus = "ended"
	CouponStatusNotStarted     CouponStatus = "not_started"
	CouponStatusExpired        CouponStatus = "expired"
	CouponStatusUsedUp         CouponStatus = "used_up"
	CouponStatusAlreadyUsed    CouponStatus = "already_used"
	CouponStatusNotInArea      CouponStatus = "not_in_area"
	CouponStatusNotForClass    CouponStatus = "not_for_class"
	CouponStatusNewRidersOnly  CouponStatus = "new_riders_only"
	CouponStatusBelowMinimum   CouponStatus = "below_minimum"
	CouponStatusBetterDiscount CouponStatus = "better_discount"
)

// PromotionSettings are the automatic discounts: on a rider's first
// completed trip, and on every LoyaltyEvery-th one. A percent of 0 (or
// LoyaltyEvery 0) turns one off; a max amount (nil: none) caps it.
type PromotionSettings struct {
	FirstRidePercent   decimal.Decimal
	FirstRideMaxAmount *decimal.Decimal
	LoyaltyEvery       int
	LoyaltyPercent     decimal.Decimal
	LoyaltyMaxAmount   *decimal.Decimal
	UpdatedBy          string
	UpdatedAt          time.Time
}

// DefaultPromotionSettings are the discounts before staff change them.
func DefaultPromotionSettings() PromotionSettings {
	return PromotionSettings{
		FirstRidePercent: decimal.NewFromInt(50),
		LoyaltyEvery:     10,
		LoyaltyPercent:   decimal.NewFromInt(20),
	}
}

// FareBreakdown's money fields are decimal.Decimal, crossing the wire
// as decimal strings (see the grpc handler) — never float64/double,
// which would reintroduce exactly the rounding drift a payments-facing
// fare calculation can't afford. DistanceKm/DurationMinutes stay
// float64: they're physical measurements from OSRM, not currency.
type FareBreakdown struct {
	// The class this fare was priced for (economy when the request named none).
	VehicleClass string
	CurrencyCode string
	// The service zone the pickup actually resolved to (see
	// CheckServiceZone) — set even when that zone has no rate card of
	// its own and pricing fell back to the global default.
	ZoneID          string
	CityID          string
	BaseFare        decimal.Decimal
	DistanceKm      float64
	DistanceFare    decimal.Decimal
	DurationMinutes float64
	DurationFare    decimal.Decimal
	// MinimumFareAdjustment is what brought base + distance + duration up to
	// the rate card's minimum; it is part of Subtotal.
	MinimumFareAdjustment decimal.Decimal
	Subtotal              decimal.Decimal
	// WaitingMinutes the driver waited at the pickup beyond the free ones,
	// and WaitingFare what they cost; added to Total, never discounted.
	WaitingMinutes       int
	WaitingFare          decimal.Decimal
	Surge                SurgeBreakdown
	SurgeAmount          decimal.Decimal
	AppliedDiscountType  DiscountType
	AppliedDiscountLabel string
	DiscountAmount       decimal.Decimal
	Total                decimal.Decimal
	// CouponStatus is what became of the request's coupon code (none when
	// there was no code).
	CouponStatus CouponStatus `json:",omitempty"`
}

// Fare is the durable record of a calculated (not estimated) fare —
// written exactly once per trip.
// FareKind is what a fare is for: the trip itself, or the fee of a trip
// that was cancelled.
type FareKind string

const (
	FareKindTrip         FareKind = "trip"
	FareKindCancellation FareKind = "cancellation"
	FareKindNoShow       FareKind = "no_show"
)

type Fare struct {
	ID        string
	TripID    string
	RiderID   string
	Kind      FareKind
	Breakdown FareBreakdown
	// QuoteID is the quote the fare came from; empty when the trip was
	// priced when it completed. ConfigID is the rate card used.
	QuoteID   string
	ConfigID  string
	CreatedAt time.Time
}

// Point is a latitude and longitude.
type Point struct {
	Latitude  float64
	Longitude float64
}

// Quote is a price for one vehicle class, held until ExpiresAt: a trip
// requested with it pays Breakdown.Total. Once a trip claims it, it is that
// trip's and no other's.
type Quote struct {
	ID           string
	RiderID      string
	ZoneID       string
	CityID       string
	VehicleClass string
	Pickup       Point
	Dropoff      Point
	// Stops on the way, in order: a trip with the quote has the same ones.
	Stops     []Point
	Breakdown FareBreakdown
	ConfigID  string
	// Coupon is the coupon the price used, if one did.
	Coupon *AppliedCoupon

	DriversAvailable bool
	PickupETAMinutes int

	CreatedAt     time.Time
	ExpiresAt     time.Time
	ClaimedTripID string
}
