package pricing

import "time"

// NOTE on money representation: fields below use float64, matching the
// float64 convention already used elsewhere in this codebase (ratings,
// coordinates). For a payments-critical system you'd normally want a
// fixed-point/integer-minor-units type to avoid floating-point rounding
// drift — that's a deliberate v1 simplification, flagged here and in
// the README, not an oversight.

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
	BaseFare                 float64
	PerKmRate                float64
	PerMinuteRate            float64
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
	SurgePercent float64
	Active       bool
}

// SurgeBreakdown lets each contributing factor stay visible instead of
// collapsing straight to a multiplier.
type SurgeBreakdown struct {
	TimeOfDayPercent float64
	DemandPercent    float64
	WeatherPercent   float64
	TotalPercent     float64
	Multiplier       float64
}

type Coupon struct {
	ID                string
	Code              string
	DiscountType      DiscountType
	DiscountValue     float64
	ValidFrom         time.Time
	ValidUntil        time.Time
	MaxRedemptions    *int
	RedemptionCount   int
	PerRiderLimit     int
	MinimumFareAmount float64
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

type FareBreakdown struct {
	CurrencyCode         string
	BaseFare             float64
	DistanceKm           float64
	DistanceFare         float64
	DurationMinutes      float64
	DurationFare         float64
	Subtotal             float64
	Surge                SurgeBreakdown
	SurgeAmount          float64
	AppliedDiscountType  DiscountType
	AppliedDiscountLabel string
	DiscountAmount       float64
	Total                float64
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
