package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// RawEvent represents a single consumed domain event, persisted verbatim
// (as raw JSON) for replay and for building future derived aggregations
// without needing to re-subscribe to the source streams.
type RawEvent struct {
	EventID    string
	EventType  string
	Payload    []byte
	OccurredAt time.Time
}

// TripStage is a named checkpoint in a trip's lifecycle, used to record at
// which point a trip was cancelled.
type TripStage string

const (
	TripStageRequested TripStage = "requested"
	TripStageAccepted  TripStage = "accepted"
	TripStageStarted   TripStage = "started"
	TripStageCompleted TripStage = "completed"
	TripStageCancelled TripStage = "cancelled"
)

// FunnelDayPoint is one day's trip funnel counts.
type FunnelDayPoint struct {
	Day            time.Time // truncated to date, UTC midnight
	RequestedCount int64
	AcceptedCount  int64
	StartedCount   int64
	CompletedCount int64
	CancelledCount int64
}

// CancellationStageCount is the number of cancellations that occurred while
// a trip was at a given stage.
type CancellationStageCount struct {
	Stage TripStage
	Count int64
}

// CancellationBreakdown aggregates cancellations across a date range.
type CancellationBreakdown struct {
	ByStage             []CancellationStageCount
	TotalCancellations  int64
	TotalTrips          int64
	CancellationRatePct decimal.Decimal
}

// RevenueDayPoint is one day's revenue totals for a given currency.
type RevenueDayPoint struct {
	Day             time.Time
	Currency        string
	GrossFareTotal  decimal.Decimal
	CommissionTotal decimal.Decimal
	TripCount       int64
}

// RevenueSummary aggregates revenue across a date range.
type RevenueSummary struct {
	Days            []RevenueDayPoint
	GrossFareTotal  decimal.Decimal
	CommissionTotal decimal.Decimal
	TotalTrips      int64
}

// RetentionCohort describes a signup cohort (by ISO week) and what fraction
// of that cohort was still active in each subsequent week.
type RetentionCohort struct {
	CohortWeek             string // ISO week, e.g. "2026-W37"
	CohortSize             int64
	RetentionPercentByWeek []decimal.Decimal // index 0 = signup week itself
}

// DateRange bounds a query; a zero Start or End means unbounded on that side.
type DateRange struct {
	Start time.Time
	End   time.Time
}
