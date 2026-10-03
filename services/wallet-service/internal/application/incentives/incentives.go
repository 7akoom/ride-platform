// Package incentives pays drivers bonuses for completing trips: a campaign
// runs for a period over a scope of trips (city, zones, class, daily hours)
// and pays, once it has ended, the bonus of the highest tier each driver
// reached, if they met its conditions (acceptance and cancellation rates,
// rating). The platform pays it into the driver's wallet.
package incentives

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// Stored statuses. Scheduled, running and ended-awaiting-settlement are
// "active" with the clock deciding which.
const (
	StatusActive    = "active"
	StatusSettling  = "settling"
	StatusSettled   = "settled"
	StatusCancelled = "cancelled"
)

// State is what a campaign looks like now.
type State string

const (
	StateScheduled State = "scheduled"
	StateRunning   State = "running"
	StateSettling  State = "settling"
	StateSettled   State = "settled"
	StateCancelled State = "cancelled"
)

// Conditions a driver must meet to be paid.
const (
	UnmetAcceptance   = "acceptance_rate"
	UnmetCancellation = "cancellation_rate"
	UnmetRating       = "rating"
)

const (
	PayoutPaid        = "paid"
	PayoutNotEligible = "not_eligible"
)

type Tier struct {
	Trips  int
	Amount wallet.Money
}

type Campaign struct {
	ID           string
	Name         string
	Description  string
	CurrencyCode string
	StartsAt     time.Time
	EndsAt       time.Time

	CityID       string
	ZoneIDs      []string
	VehicleClass string
	DailyStart   int
	DailyEnd     int
	TimeZone     string

	// nil means no condition.
	MinAcceptanceRate   *wallet.Money
	MaxCancellationRate *wallet.Money
	MinRating           *wallet.Money

	Tiers []Tier

	Status       string
	CancelReason string
	CreatedAt    time.Time
	SettledAt    time.Time

	PaidDrivers int
	PaidTotal   wallet.Money
}

// StateAt is the campaign's state at now.
func (c Campaign) StateAt(now time.Time) State {
	switch c.Status {
	case StatusCancelled:
		return StateCancelled
	case StatusSettled:
		return StateSettled
	case StatusSettling:
		return StateSettling
	}

	switch {
	case now.Before(c.StartsAt):
		return StateScheduled
	case now.Before(c.EndsAt):
		return StateRunning
	default:
		return StateSettling
	}
}

// LowestTier is the fewest trips that pay anything.
func (c Campaign) LowestTier() int {
	if len(c.Tiers) == 0 {
		return 0
	}

	return c.Tiers[0].Trips
}

// Activity is what a driver did in a campaign's scope.
type Activity struct {
	DriverID            string
	CompletedTrips      int
	OffersAccepted      int
	OffersDeclined      int
	DriverCancellations int
}

// Payout is what was decided for one driver of a campaign.
type Payout struct {
	CampaignID       string
	DriverID         string
	CompletedTrips   int
	AcceptanceRate   wallet.Money
	CancellationRate wallet.Money
	Rating           *wallet.Money
	TierTrips        int
	Amount           wallet.Money
	Status           string
	Unmet            []string
	CreatedAt        time.Time
}

var hundred = decimal.NewFromInt(100)

// Rates works out the acceptance rate (accepted of all offers answered or
// let expire; 100 without offers) and the cancellation rate (cancelled of
// accepted trips that ended; 0 without any), in percent, to 2 places.
func Rates(a Activity) (acceptance, cancellation wallet.Money) {
	acceptance = hundred

	if offers := a.OffersAccepted + a.OffersDeclined; offers > 0 {
		acceptance = decimal.NewFromInt(int64(a.OffersAccepted)).Mul(hundred).Div(decimal.NewFromInt(int64(offers))).Round(2)
	}

	cancellation = decimal.Zero

	if ended := a.CompletedTrips + a.DriverCancellations; ended > 0 {
		cancellation = decimal.NewFromInt(int64(a.DriverCancellations)).Mul(hundred).Div(decimal.NewFromInt(int64(ended))).Round(2)
	}

	return acceptance, cancellation
}

// Reached is the highest tier a number of trips reaches, and the next one.
func (c Campaign) Reached(trips int) (reached, next *Tier) {
	for i := range c.Tiers {
		if trips >= c.Tiers[i].Trips {
			reached = &c.Tiers[i]
		} else {
			return reached, &c.Tiers[i]
		}
	}

	return reached, nil
}

// Unmet lists the conditions a driver misses. rating is nil for a driver
// nobody has rated yet, who is not held to a minimum rating.
func (c Campaign) Unmet(acceptance, cancellation wallet.Money, rating *wallet.Money) []string {
	var unmet []string

	if c.MinAcceptanceRate != nil && acceptance.LessThan(*c.MinAcceptanceRate) {
		unmet = append(unmet, UnmetAcceptance)
	}

	if c.MaxCancellationRate != nil && cancellation.GreaterThan(*c.MaxCancellationRate) {
		unmet = append(unmet, UnmetCancellation)
	}

	if c.MinRating != nil && rating != nil && rating.LessThan(*c.MinRating) {
		unmet = append(unmet, UnmetRating)
	}

	return unmet
}

// Decide works out a driver's payout from their activity and rating.
func (c Campaign) Decide(a Activity, rating *wallet.Money) Payout {
	acceptance, cancellation := Rates(a)
	reached, _ := c.Reached(a.CompletedTrips)

	payout := Payout{
		CampaignID:       c.ID,
		DriverID:         a.DriverID,
		CompletedTrips:   a.CompletedTrips,
		AcceptanceRate:   acceptance,
		CancellationRate: cancellation,
		Rating:           rating,
		Amount:           decimal.Zero,
		Status:           PayoutNotEligible,
		Unmet:            c.Unmet(acceptance, cancellation, rating),
	}

	if reached != nil {
		payout.TierTrips = reached.Trips

		if len(payout.Unmet) == 0 {
			payout.Amount = reached.Amount
			payout.Status = PayoutPaid
		}
	}

	return payout
}

// CampaignsQuery selects one page of campaigns, newest first.
type CampaignsQuery struct {
	Status  string
	AfterID string
	Limit   int
	Now     time.Time
}

// Store keeps campaigns and their payouts. Settle pays a driver in the
// same transaction that records the payout.
type Store interface {
	Config(ctx context.Context) (wallet.Config, error)
	Create(ctx context.Context, c Campaign, createdBy, idempotencyKey string) (Campaign, error)
	// FindByKey reports a campaign made with that key earlier.
	FindByKey(ctx context.Context, key string) (Campaign, bool, error)
	// Get returns ErrCampaignNotFound.
	Get(ctx context.Context, id string) (Campaign, error)
	List(ctx context.Context, query CampaignsQuery) ([]Campaign, error)
	// Cancel cancels an active campaign that has not ended at now;
	// ErrNotCancellable otherwise.
	Cancel(ctx context.Context, id, reason string, now time.Time) (Campaign, error)
	// ClaimDue takes one campaign that ended before endedBefore and is not
	// settled, for lease; found is false when none is due.
	ClaimDue(ctx context.Context, endedBefore time.Time, lease time.Duration) (c Campaign, found bool, err error)
	// Settle records the payout and, when paid, credits the driver's wallet
	// with the event that tells them. A payout recorded earlier is returned
	// as it was.
	Settle(ctx context.Context, c Campaign, payout Payout) (Payout, error)
	MarkSettled(ctx context.Context, id string, at time.Time) error
	Payouts(ctx context.Context, campaignID, afterDriverID string, limit int) ([]Payout, error)
	// DriverPayouts returns the driver's payouts of these campaigns, by campaign.
	DriverPayouts(ctx context.Context, driverID string, campaignIDs []string) (map[string]Payout, error)
	// Visible returns campaigns not cancelled that end after endedAfter, by start.
	Visible(ctx context.Context, endedAfter time.Time) ([]Campaign, error)
}

// Trips counts drivers' activity in a campaign's scope (trip-service).
type Trips interface {
	Driver(ctx context.Context, driverID string, c Campaign) (Activity, error)
	Drivers(ctx context.Context, c Campaign, minCompleted int, pageToken string) (page []Activity, next string, err error)
}

// Profile is what incentives need of a driver.
type Profile struct {
	VehicleClass string
	// Rating is nil while nobody has rated the driver.
	Rating *wallet.Money
}

// Drivers reads drivers' class and rating (driver-service).
type Drivers interface {
	Profile(ctx context.Context, driverID string) (Profile, error)
}

var (
	ErrCampaignNotFound  = errors.New("incentive campaign not found")
	ErrInvalidName       = errors.New("name is required (up to 80 characters), description up to 500")
	ErrInvalidPeriod     = errors.New("the campaign must end after it starts, in the future, at most 31 days later, and not start in the past")
	ErrInvalidScope      = errors.New("city_id and zone_ids must be ids, vehicle_class economy or comfort")
	ErrInvalidHours      = errors.New("daily_start and daily_end must both be HH:MM, or both empty")
	ErrInvalidTimeZone   = errors.New("time_zone is not an IANA time zone")
	ErrInvalidConditions = errors.New("rates must be percentages 0-100 and the rating 1-5")
	ErrInvalidTiers      = errors.New("1-5 tiers, with trips (1-1000) and amounts (positive, at most 3 decimals) both increasing")
	ErrIdempotencyKey    = errors.New("idempotency_key is up to 120 characters")
	ErrKeyReused         = errors.New("this idempotency_key was already used for another campaign")
	ErrNotCancellable    = errors.New("only a campaign that has not ended can be cancelled")
	ErrReasonRequired    = errors.New("a reason is required (up to 500 characters)")
	ErrDriverNotFound    = errors.New("driver not found")
	ErrInvalidPage       = errors.New("page_size or page_token is not valid")
	ErrInvalidStatus     = errors.New("status is not valid")
)
