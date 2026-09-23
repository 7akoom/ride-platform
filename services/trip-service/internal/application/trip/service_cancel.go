package trip

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultNoShowWait is how long a driver waits at the pickup after marking
// arrival before they may cancel because the rider did not come.
const DefaultNoShowWait = 5 * time.Minute

// CancelInput is a cancellation: who asks and why. RiderNoShow is the
// driver cancelling because the rider did not come to the pickup.
type CancelInput struct {
	TripID      string
	Reason      string
	By          CancelledBy
	RiderNoShow bool
}

// CancelRecord is what the repository stores for a cancellation. Allow is
// checked against the locked trip before it is cancelled.
type CancelRecord struct {
	TripID      string
	Reason      string
	By          CancelledBy
	RiderNoShow bool
	Allow       func(current Trip) error
}

// WithNoShowWait sets how long a driver must wait at the pickup before
// reporting a no-show (DefaultNoShowWait when not positive).
func WithNoShowWait(wait time.Duration) Option {
	return func(s *service) {
		if wait > 0 {
			s.noShowWait = wait
		}
	}
}

func (s *service) CancelTrip(
	ctx context.Context,
	input CancelInput,
) (Trip, error) {
	trimmedID := strings.TrimSpace(input.TripID)
	if trimmedID == "" {
		return Trip{}, ErrTripIDRequired
	}

	if !input.By.Valid() {
		return Trip{}, ErrInvalidCancelledBy
	}

	record := CancelRecord{
		TripID:      trimmedID,
		Reason:      strings.TrimSpace(input.Reason),
		By:          input.By,
		RiderNoShow: input.RiderNoShow,
		Allow:       func(Trip) error { return nil },
	}

	if input.RiderNoShow {
		if input.By != CancelledByDriver {
			return Trip{}, ErrNoShowOnlyByDriver
		}

		record.Allow = s.noShowAllowed
	}

	cancelled, err := s.repository.Cancel(ctx, record)
	if err != nil {
		return Trip{}, fmt.Errorf("cancel trip: %w", err)
	}

	return cancelled, nil
}

// noShowAllowed: the driver marked arrival and has waited long enough since.
func (s *service) noShowAllowed(current Trip) error {
	if current.Status != StatusAccepted || current.ArrivedAt == nil {
		return ErrNoShowTooEarly
	}

	if s.now().Sub(*current.ArrivedAt) < s.noShowWait {
		return ErrNoShowTooEarly
	}

	return nil
}
