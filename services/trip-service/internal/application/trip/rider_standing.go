package trip

import (
	"context"
	"errors"
	"fmt"
)

// Standing is whether a rider may request trips, and what they owe.
type Standing struct {
	CanRequestTrips bool
	// Outstanding is a decimal string in CurrencyCode.
	Outstanding  string
	CurrencyCode string
}

// RiderStanding reads a rider's standing (wallet-service). An error that
// wraps ErrUpstreamUnavailable means it could not be read.
type RiderStanding interface {
	RiderStanding(ctx context.Context, riderID string) (Standing, error)
}

// WithRiderStanding refuses a trip to a rider who owes cancelled trips' fees
// when the deployment blocks trips until they are paid.
func WithRiderStanding(standing RiderStanding) Option {
	if standing == nil {
		panic("rider standing is required")
	}

	return func(s *service) {
		s.standing = standing
	}
}

// checkRiderStanding refuses a rider who may not request trips. While the
// standing cannot be read the rider is let through: an unpaid fee is still
// collected from the next money that reaches their wallet, so a wallet-service
// outage should not stop every trip.
func (s *service) checkRiderStanding(ctx context.Context, riderID string) error {
	if s.standing == nil {
		return nil
	}

	standing, err := s.standing.RiderStanding(ctx, riderID)

	switch {
	case errors.Is(err, ErrUpstreamUnavailable):
		return nil
	case err != nil:
		return fmt.Errorf("check rider standing: %w", err)
	case !standing.CanRequestTrips:
		return fmt.Errorf("%w (%s %s)", ErrRiderOwesFees, standing.Outstanding, standing.CurrencyCode)
	}

	return nil
}
