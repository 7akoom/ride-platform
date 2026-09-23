// Package tariffs is how staff set prices: rate cards for everywhere, a
// city or a zone, surge rules by the hour, and surges on one zone for a
// while. The fare pipeline that reads them lives in package pricing.
package tariffs

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

var (
	ErrCityNotFound      = errors.New("city not found")
	ErrZoneNotFound      = errors.New("zone not found")
	ErrRateCardNotFound  = errors.New("there is no rate card there to retire")
	ErrCannotRetireBase  = errors.New("the rate card for every class everywhere cannot be retired")
	ErrNoBaseRateCard    = errors.New("the rate card for every class everywhere is missing")
	ErrSurgeRuleNotFound = errors.New("surge rule not found")
	ErrZoneSurgeNotFound = errors.New("zone surge not found")
	ErrZoneSurgeOver     = errors.New("the zone surge is already over")
	ErrPlacesUnavailable = errors.New("cities and zones could not be checked")
)

// InvalidError names the field a request got wrong and why.
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Reason }

func invalid(field, reason string) error { return &InvalidError{Field: field, Reason: reason} }

// Places checks cities and zones exist (location-service). Inactive ones
// count: staff may price a city before it opens.
type Places interface {
	CityExists(ctx context.Context, cityID string) (bool, error)
	ZoneExists(ctx context.Context, zoneID string) (bool, error)
}

// Filter narrows a listing to one city's or one zone's entries.
type Filter struct {
	CityID string
	ZoneID string
}

// Repository stores what staff set.
type Repository interface {
	// CurrentRateCards returns the newest version of every place's and
	// class's card that is not retired.
	CurrentRateCards(ctx context.Context, filter Filter) ([]pricing.Config, error)
	// LatestRateCard returns the newest version for exactly this place and
	// class, retired or not.
	LatestRateCard(ctx context.Context, scope pricing.Scope) (pricing.Config, bool, error)
	// InsertRateCard adds a version.
	InsertRateCard(ctx context.Context, card pricing.Config) (pricing.Config, error)

	ListSurgeRules(ctx context.Context, filter Filter) ([]pricing.SurgeTimeRule, error)
	CreateSurgeRule(ctx context.Context, rule pricing.SurgeTimeRule) (pricing.SurgeTimeRule, error)
	// UpdateSurgeRule changes label, day, hours and percent; SetSurgeRuleActive
	// switches a rule. Both return ErrSurgeRuleNotFound.
	UpdateSurgeRule(ctx context.Context, rule pricing.SurgeTimeRule) (pricing.SurgeTimeRule, error)
	SetSurgeRuleActive(ctx context.Context, ruleID string, active bool) (pricing.SurgeTimeRule, error)

	// ListZoneSurges returns the running and coming ones, and with
	// includePast the latest 100 of all.
	ListZoneSurges(ctx context.Context, zoneID string, includePast bool, now time.Time) ([]pricing.ZoneSurge, error)
	CreateZoneSurge(ctx context.Context, surge pricing.ZoneSurge) (pricing.ZoneSurge, error)
	FindZoneSurge(ctx context.Context, surgeID string) (pricing.ZoneSurge, error)
	// EndZoneSurge sets ended_at if the surge is not over; it returns
	// ErrZoneSurgeOver otherwise.
	EndZoneSurge(ctx context.Context, surgeID string, now time.Time) (pricing.ZoneSurge, error)
}

// RateCardInput is a whole card for one place and class. Money is decimal;
// MaxSurgePercent nil means the default (150).
type RateCardInput struct {
	Scope                    pricing.Scope
	BaseFare                 decimal.Decimal
	PerKmRate                decimal.Decimal
	PerMinuteRate            decimal.Decimal
	MinimumFare              decimal.Decimal
	FreeWaitingMinutes       int
	WaitingPerMinute         decimal.Decimal
	CancellationFee          decimal.Decimal
	CancellationGraceMinutes int
	NoShowFee                decimal.Decimal
	MaxSurgePercent          *decimal.Decimal
	DemandSurge              bool
	WeatherSurge             bool
}

// SurgeRuleInput is a rule's label, days, hours and percent, and on create
// where it applies.
type SurgeRuleInput struct {
	ZoneID       string
	CityID       string
	Label        string
	DayOfWeek    *int
	StartTime    string
	EndTime      string
	SurgePercent decimal.Decimal
}

// ZoneSurgeInput starts a surge on a zone.
type ZoneSurgeInput struct {
	ZoneID          string
	SurgePercent    decimal.Decimal
	Reason          string
	StartsAt        time.Time // zero means now
	DurationMinutes int
}
