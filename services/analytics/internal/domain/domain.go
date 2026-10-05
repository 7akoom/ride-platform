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
}

type RevenueSummary struct {
	Currency        string
	GrossFareTotal  decimal.Decimal
	CommissionTotal decimal.Decimal
	TotalTrips      int64
	FeeTotal        decimal.Decimal
	TotalFees       int64
}

// RetentionCohort is the people who started in one week (Monday, local
// clock) and the share active in each week since, week 0 first.
type RetentionCohort struct {
	CohortWeek             string
	CohortSize             int64
	RetentionPercentByWeek []decimal.Decimal
}
