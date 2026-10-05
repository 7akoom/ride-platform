package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ErrInvalidArgument marks a report request the caller has to fix.
var ErrInvalidArgument = errors.New("invalid argument")

// ErrUnknownCity is a city_id the location service does not know.
var ErrUnknownCity = errors.New("unknown city")

// ErrUpstreamUnavailable is a peer service that could not answer.
var ErrUpstreamUnavailable = errors.New("upstream unavailable")

// CancelStage is how far a trip had got when it was cancelled.
type CancelStage string

const (
	CancelStageRequested CancelStage = "requested"
	CancelStageAccepted  CancelStage = "accepted"
	CancelStageArrived   CancelStage = "arrived"
	CancelStageStarted   CancelStage = "started"
)

// CancelStages lists every stage in trip order.
var CancelStages = []CancelStage{CancelStageRequested, CancelStageAccepted, CancelStageArrived, CancelStageStarted}

// Fare kinds, as pricing-service names them.
const (
	FareKindTrip         = "trip"
	FareKindCancellation = "cancellation"
	FareKindNoShow       = "no_show"
)

// DateRange is what the caller asked for: local days (From/To, YYYY-MM-DD)
// or instants (Start/End), any of them empty.
type DateRange struct {
	Start time.Time
	End   time.Time
	From  string
	To    string
}

// Scope narrows a report to some trips. Empty fields do not filter.
type Scope struct {
	CityID       string
	ZoneID       string
	VehicleClass string
}

// Window is a resolved report range: local days From..To (inclusive) on
// Location, and the instants [Start, End) they cover.
type Window struct {
	From     time.Time
	To       time.Time
	Location *time.Location
	Start    time.Time
	End      time.Time
}

// Days is the number of days in the window.
func (w Window) Days() int {
	return int(w.To.Sub(w.From).Hours()/24) + 1
}

// FunnelDayPoint is the trips requested on one day and how far they got.
type FunnelDayPoint struct {
	Day            time.Time
	RequestedCount int64
	AcceptedCount  int64
	StartedCount   int64
	CompletedCount int64
	CancelledCount int64
}

type CancellationStageCount struct {
	Stage CancelStage
	Count int64
}

type CancellationByCount struct {
	CancelledBy string
	Count       int64
}

type CancellationBreakdown struct {
	ByStage             []CancellationStageCount
	ByCancelledBy       []CancellationByCount
	RiderNoShows        int64
	TotalCancellations  int64
	TotalTrips          int64
	CancellationRatePct decimal.Decimal
}

// RevenueDayPoint is one day's fares in one currency.
type RevenueDayPoint struct {
	Day             time.Time
	Currency        string
	GrossFareTotal  decimal.Decimal
	CommissionTotal decimal.Decimal
	TripCount       int64
	FeeTotal        decimal.Decimal
	FeeCount        int64
	RevenueExtras
}

type RevenueSummary struct {
	Currency        string
	GrossFareTotal  decimal.Decimal
	CommissionTotal decimal.Decimal
	TotalTrips      int64
	FeeTotal        decimal.Decimal
	TotalFees       int64
	RevenueExtras
}

// RetentionCohort is the people who started in one week (Monday, local
// clock) and the share active in each week since, week 0 first.
type RetentionCohort struct {
	CohortWeek             string
	CohortSize             int64
	RetentionPercentByWeek []decimal.Decimal
}

// RevenueExtras are the parts of trip fares a revenue day also reports.
type RevenueExtras struct {
	DiscountTotal decimal.Decimal
	SurgeTotal    decimal.Decimal
}

// DurationStats is how long a step took, in seconds, over the trips that
// reached it.
type DurationStats struct {
	Count   int64
	Average int64
	Median  int64
	P90     int64
}

// ServiceLevelDay is the trips requested on one day (zero Day: the whole
// range) and how fast they were matched, reached and driven.
type ServiceLevelDay struct {
	Day       time.Time
	Requested int64
	Completed int64
	Match     DurationStats
	Pickup    DurationStats
	Ride      DurationStats
}

// OfferDay is the offers sent on one day and how they ended.
type OfferDay struct {
	Day      time.Time
	Offered  int64
	Accepted int64
	Rejected int64
	Expired  int64
	Pending  int64
}

// RatingSummary: ByStars[0] is one star, ByStars[4] five.
type RatingSummary struct {
	Count   int64
	Average decimal.Decimal
	ByStars [5]int64
}

type Ratings struct {
	Drivers        RatingSummary
	Riders         RatingSummary
	CompletedTrips int64
}

// LiveTrips are trips under way now.
type LiveTrips struct {
	Waiting    int64
	OnTheWay   int64
	InProgress int64
}

// LedgerLine is movements of one type into or out of one kind of wallet.
type LedgerLine struct {
	OwnerType string
	Type      string
	Entries   int64
	Credited  decimal.Decimal
	Debited   decimal.Decimal
}

// LedgerDay is one local day of the ledger.
type LedgerDay struct {
	Day   time.Time
	Lines []LedgerLine
}

// MoneyHeld is what wallets hold now.
type MoneyHeld struct {
	RiderBalances    decimal.Decimal
	DriverCredit     decimal.Decimal
	DriverDebt       decimal.Decimal
	SuspendedDrivers int64
	RiderDues        decimal.Decimal
}

// Ledger is the wallets' ledger over a range, as wallet-service sums it.
type Ledger struct {
	Days     []LedgerDay
	Currency string
	Held     MoneyHeld
}

// MoneyHeadline is the figures most dashboards show.
type MoneyHeadline struct {
	ToppedUp         decimal.Decimal
	VouchersRedeemed decimal.Decimal
	Commission       decimal.Decimal
	Tips             decimal.Decimal
	Refunds          decimal.Decimal
	Incentives       decimal.Decimal
	PaidOut          decimal.Decimal
	Transferred      decimal.Decimal
	AdjustmentsIn    decimal.Decimal
	AdjustmentsOut   decimal.Decimal
}

// MoneyFlows is the ledger report.
type MoneyFlows struct {
	Days     []LedgerDay
	Totals   []LedgerLine
	Headline MoneyHeadline
	Held     MoneyHeld
	Currency string
}

// DriverSupply is how many drivers of one class are in one state now.
type DriverSupply struct {
	Status       string
	Availability string
	VehicleClass string
	Drivers      int64
}

// ClassSupply is approved drivers of one class by availability.
type ClassSupply struct {
	VehicleClass string
	Available    int64
	Busy         int64
	Offline      int64
}

// LiveOverview is the platform right now.
type LiveOverview struct {
	Trips          LiveTrips
	Today          FunnelDayPoint
	TodayGrossFare decimal.Decimal
	Currency       string
	Available      int64
	Busy           int64
	Offline        int64
	PendingReview  int64
	ByClass        []ClassSupply
	AsOf           time.Time
}
