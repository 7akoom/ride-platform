package trip

import "time"

type Status string

const (
	StatusRequested  Status = "requested"
	StatusAccepted   Status = "accepted"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusCancelled  Status = "cancelled"
)

// CanTransitionTo encodes the trip state machine in one place, so every
// use case (accept/start/complete/cancel) checks the same rules instead
// of duplicating "if status is X" logic. The allowed transitions:
//
//	requested   -> accepted, cancelled
//	accepted    -> in_progress, cancelled
//	in_progress -> completed, cancelled
//	completed   -> (terminal, no transitions out)
//	cancelled   -> (terminal, no transitions out)
func (s Status) CanTransitionTo(target Status) bool {
	switch s {
	case StatusRequested:
		return target == StatusAccepted || target == StatusCancelled
	case StatusAccepted:
		return target == StatusInProgress || target == StatusCancelled
	case StatusInProgress:
		return target == StatusCompleted || target == StatusCancelled
	default:
		return false
	}
}

// CancelledBy is who cancelled a trip.
type CancelledBy string

const (
	CancelledByRider  CancelledBy = "rider"
	CancelledByDriver CancelledBy = "driver"
	// CancelledBySystem is a service (dispatch finding no driver) or staff.
	CancelledBySystem CancelledBy = "system"
)

func (b CancelledBy) Valid() bool {
	switch b {
	case CancelledByRider, CancelledByDriver, CancelledBySystem:
		return true
	default:
		return false
	}
}

// Coordinates is a validated lat/lng pair.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

func NewCoordinates(latitude, longitude float64) (Coordinates, error) {
	if latitude < -90 || latitude > 90 {
		return Coordinates{}, ErrInvalidLatitude
	}

	if longitude < -180 || longitude > 180 {
		return Coordinates{}, ErrInvalidLongitude
	}

	return Coordinates{
		Latitude:  latitude,
		Longitude: longitude,
	}, nil
}

// Trip is the aggregate root for the trip domain — the record of one
// ride from request through to completion or cancellation.
type Trip struct {
	ID                 string
	RiderID            string
	DriverID           string
	Status             Status
	Pickup             Coordinates
	Dropoff            Coordinates
	CancellationReason string
	// Who cancelled, and whether the driver cancelled because the rider did
	// not come.
	CancelledBy   CancelledBy
	RiderNoShow   bool
	VehicleClass  string
	PaymentMethod string
	// The addresses as the rider picked them, and what the saved pickup
	// address tells the captain.
	PickupAddress      string
	DropoffAddress     string
	PickupDetails      string
	PickupNote         string
	PickupPhotoMediaID string
	// The fare quote the trip was requested with and the price it fixed
	// (a decimal string in CurrencyCode); empty without one.
	QuoteID      string
	QuotedFare   string
	CurrencyCode string
	// A trip booked for someone else: who the driver picks up.
	PassengerName  string
	PassengerPhone string
	// Scheduled: it was booked ahead (its id is the scheduled trip's).
	Scheduled   bool
	RequestedAt time.Time
	AcceptedAt  *time.Time
	// ArrivedAt is when the driver said they were at the pickup.
	ArrivedAt   *time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SosTriggeredBy is who pressed the SOS button — the rider or the
// driver both can, and the safety team needs to know which.
type SosTriggeredBy string

const (
	SosTriggeredByRider  SosTriggeredBy = "rider"
	SosTriggeredByDriver SosTriggeredBy = "driver"
)

func (t SosTriggeredBy) Valid() bool {
	switch t {
	case SosTriggeredByRider, SosTriggeredByDriver:
		return true
	default:
		return false
	}
}

// Waypoint is one recorded point along a trip's path.
type Waypoint struct {
	Coordinates Coordinates
	RecordedAt  time.Time
}
