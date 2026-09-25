// Package schedule is trips booked ahead: a rider books one for a time, and
// shortly before it the scheduler requests the trip (with the booking's id)
// through the trip service like any other.
package schedule

import (
	"context"
	"errors"
	"time"

	// The pickup city's time zone is shown with every booking; the binary
	// carries the zone database so a minimal image needs none.
	_ "time/tzdata"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

var (
	ErrRiderRequired   = errors.New("rider_id is required")
	ErrIdempotencyKey  = errors.New("idempotency_key is required (1-120 characters)")
	ErrKeyReused       = errors.New("this idempotency_key was already used for another booking")
	ErrTooSoon         = errors.New("a trip is booked at least 30 minutes ahead; request it now instead")
	ErrTooFar          = errors.New("a trip is booked at most 7 days ahead")
	ErrTooManyUpcoming = errors.New("you already have as many upcoming scheduled trips as allowed")
	ErrNotFound        = errors.New("scheduled trip not found")
	// ErrNotScheduled: it was dispatched, cancelled or it failed already.
	ErrNotScheduled = errors.New("the scheduled trip is not waiting any more (dispatched, cancelled or failed)")
)

// Status is where a booking stands.
type Status string

const (
	Scheduled  Status = "scheduled"
	Dispatched Status = "dispatched"
	Cancelled  Status = "cancelled"
	Failed     Status = "failed"
)

// Ride is a trip booked ahead.
type Ride struct {
	ID             string
	RiderID        string
	IdempotencyKey string
	Status         Status
	ScheduledAt    time.Time
	TimeZone       string

	Pickup                trip.Coordinates
	Dropoff               trip.Coordinates
	PickupAddress         string
	DropoffAddress        string
	PickupSavedAddressID  string
	DropoffSavedAddressID string
	VehicleClass          string
	PaymentMethod         string
	PassengerName         string
	PassengerPhone        string
	// Stops on the way, in order.
	Stops []trip.Stop

	NextAttemptAt time.Time
	Attempts      int
	LastError     string

	TripID       string
	CreatedAt    time.Time
	DispatchedAt *time.Time
	CancelledAt  *time.Time
	FailedAt     *time.Time
}

// Local is the time in the pickup city ("2026-09-26 08:30").
func (r Ride) Local() string {
	location, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		location = time.UTC
	}

	return r.ScheduledAt.In(location).Format("2006-01-02 15:04")
}

// Store keeps bookings.
type Store interface {
	// Create stores a new booking, counting the rider's upcoming ones under a
	// lock on the rider (ErrTooManyUpcoming past maxUpcoming). When the
	// rider's key was used already it stores nothing and returns that
	// booking, with true.
	Create(ctx context.Context, ride Ride, maxUpcoming int) (Ride, bool, error)
	FindByKey(ctx context.Context, riderID, key string) (Ride, bool, error)
	// ListForRider: upcoming (scheduled) soonest first, or the latest of any
	// status newest first.
	ListForRider(ctx context.Context, riderID string, includePast bool, limit int) ([]Ride, error)
	// Cancel cancels the rider's booking that is still scheduled;
	// ErrNotFound for anyone else's, ErrNotScheduled otherwise.
	Cancel(ctx context.Context, id, riderID string, now time.Time) (Ride, error)

	// ClaimDue takes up to limit bookings due by now and pushes their next
	// attempt past lease, so no other scheduler takes them meanwhile.
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Ride, error)
	// MarkDispatched records the trip; ErrNotScheduled when the booking was
	// cancelled meanwhile.
	MarkDispatched(ctx context.Context, id, tripID string, now time.Time) error
	// Retry records why it could not be dispatched and when to try again.
	Retry(ctx context.Context, id, reason string, next time.Time) error
	// Fail gives up (with the trip.schedule_failed event for the rider's
	// notification).
	Fail(ctx context.Context, id, reason string, now time.Time) error
}

// ZoneLocator tells whether a pickup is served, and the city's time zone.
type ZoneLocator interface {
	Locate(ctx context.Context, latitude, longitude float64) (served bool, timeZone string, err error)
}

// Trips is the trip service the scheduler requests trips from.
type Trips interface {
	RequestTrip(ctx context.Context, input trip.RequestTripInput) (trip.Trip, error)
	GetTrip(ctx context.Context, tripID string) (trip.Trip, error)
	CancelTrip(ctx context.Context, input trip.CancelInput) (trip.Trip, error)
}

// Limits are the deployment's rules for booking ahead.
type Limits struct {
	MinAhead     time.Duration
	MaxAhead     time.Duration
	MaxUpcoming  int
	DispatchLead time.Duration
	// Grace is how long after the time a booking that could not be
	// dispatched is still tried.
	Grace time.Duration
}
