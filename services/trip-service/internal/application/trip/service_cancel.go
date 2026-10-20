package trip

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultNoShowWait is how long a driver waits at the pickup after marking
// arrival before they may cancel because the rider did not come, when the
// trip's rate card cannot be read.
const DefaultNoShowWait = 6 * time.Minute

// NoShowWaits reads the no-show wait of a trip's rate card (pricing-service),
// which staff set with the prices.
type NoShowWaits interface {
	NoShowWait(ctx context.Context, tripID, riderID string) (time.Duration, error)
}

// WithNoShowWaits makes each trip wait as long as its rate card says; the
// configured wait (WithNoShowWait) stays the fallback.
func WithNoShowWaits(source NoShowWaits) Option {
	return func(s *service) {
		s.noShowWaits = source
	}
}

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
		// The driver reports it, or the no-show sweeper does once the wait ran out.
		if input.By != CancelledByDriver && input.By != CancelledBySystem {
			return Trip{}, ErrNoShowOnlyByDriver
		}

		wait := s.noShowWaitOf(ctx, trimmedID)
		record.Allow = func(current Trip) error { return s.noShowAllowed(current, wait) }
	}

	cancelled, err := s.repository.Cancel(ctx, record)
	if err != nil {
		return Trip{}, fmt.Errorf("cancel trip: %w", err)
	}

	return cancelled, nil
}

// noShowWaitOf is the trip's rate card's no-show wait, or the configured one
// when there is no card to ask or it cannot be read.
func (s *service) noShowWaitOf(ctx context.Context, tripID string) time.Duration {
	if s.noShowWaits == nil {
		return s.noShowWait
	}

	current, err := s.repository.FindByID(ctx, tripID)
	if err != nil {
		return s.noShowWait
	}

	wait, err := s.noShowWaits.NoShowWait(ctx, tripID, current.RiderID)
	if err != nil || wait <= 0 {
		return s.noShowWait
	}

	return wait
}

// noShowAllowed: the driver marked arrival and has waited long enough since.
func (s *service) noShowAllowed(current Trip, wait time.Duration) error {
	if current.Status != StatusAccepted || current.ArrivedAt == nil {
		return ErrNoShowTooEarly
	}

	if s.now().Sub(*current.ArrivedAt) < wait {
		return ErrNoShowTooEarly
	}

	return nil
}
