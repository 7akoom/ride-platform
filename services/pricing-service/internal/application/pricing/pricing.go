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

// Config is the active rate card for this deployment. One deployment =
// one currency (matches the "sell a separate instance per client"
// business model — no need for multi-currency inside one instance).
type Config struct {
	ID                       string
	CurrencyCode             string
	BaseFare                 decimal.Decimal
	PerKmRate                decimal.Decimal
	PerMinuteRate            decimal.Decimal
	AverageSpeedKmh          float64
	DistanceCorrectionFactor float64
	CreatedAt                time.Time
}

// SurgeTimeRule is one entry in the configurable peak-hours schedule.
// DayOfWeek is nil for "every day" (matches Postgres EXTRACT(DOW...):
// 0=Sunday..6=Saturday).
type SurgeTimeRule struct {
	ID           string
	Label        string
	DayOfWeek    *int
	StartTime    string // "HH:MM:SS", kept as string — no calendar date involved
	EndTime      string
	SurgePercent decimal.Decimal
	Active       bool
}

// SurgeBreakdown lets each contributing factor stay visible instead of
// collapsing straight to a multiplier. Kept as decimal, not float64,
// same as every other value that feeds directly into money math below
// (see FareBreakdown) — a percentage that multiplies a fare is exactly
// as precision-sensitive as the fare itself.
type SurgeBreakdown struct {
	TimeOfDayPercent decimal.Decimal
	DemandPercent    decimal.Decimal
	WeatherPercent   decimal.Decimal
	TotalPercent     decimal.Decimal
	Multiplier       decimal.Decimal
}

type Coupon struct {
	ID                string
	Code              string
	DiscountType      DiscountType
	DiscountValue     decimal.Decimal
	ValidFrom         time.Time
	ValidUntil        time.Time
	MaxRedemptions    *int
	RedemptionCount   int
	PerRiderLimit     int
	MinimumFareAmount decimal.Decimal
	Active            bool
}

// IsCurrentlyValid checks time window and total-redemption cap only —
// per-rider usage is checked separately since it needs a DB lookup.
func (c Coupon) IsCurrentlyValid(now time.Time) bool {
	if !c.Active {
		return false
	}

	if now.Before(c.ValidFrom) || now.After(c.ValidUntil) {
		return false
	}

	if c.MaxRedemptions != nil && c.RedemptionCount >= *c.MaxRedemptions {
		return false
	}

	return true
}

// FareBreakdown's money fields are decimal.Decimal, crossing the wire
// as decimal strings (see the grpc handler) — never float64/double,
// which would reintroduce exactly the rounding drift a payments-facing
// fare calculation can't afford. DistanceKm/DurationMinutes stay
// float64: they're physical measurements from OSRM, not currency.
type FareBreakdown struct {
	CurrencyCode         string
	BaseFare             decimal.Decimal
	DistanceKm           float64
	DistanceFare         decimal.Decimal
	DurationMinutes      float64
	DurationFare         decimal.Decimal
	Subtotal             decimal.Decimal
	Surge                SurgeBreakdown
	SurgeAmount          decimal.Decimal
	AppliedDiscountType  DiscountType
	AppliedDiscountLabel string
	DiscountAmount       decimal.Decimal
	Total                decimal.Decimal
}

// Fare is the durable record of a calculated (not estimated) fare —
// written exactly once per trip.
type Fare struct {
	ID        string
	TripID    string
	RiderID   string
	Breakdown FareBreakdown
	CreatedAt time.Time
}
