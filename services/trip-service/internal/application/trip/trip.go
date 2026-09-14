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
	RequestedAt        time.Time
	AcceptedAt         *time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	CancelledAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
