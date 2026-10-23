package dispatch

import (
	"context"
	"time"
)

// The offer times staff may set (pricing-service checks them too); anything else
// read from the settings is ignored and the instance's own time is used.
const (
	minOfferDuration = 5 * time.Second
	maxOfferDuration = 60 * time.Second
)

// OfferDurationSource reads how long staff want a driver to have to accept an offer
// (pricing-service dispatch settings); zero when they left it to the instance.
type OfferDurationSource interface {
	OfferDuration(ctx context.Context) (time.Duration, error)
}

// WithOfferDurations lets staff change how long a driver has to accept an offer
// from the admin panel. It only changes the time of an instance that works by
// offers (WithOffers): it never turns offers on. Without it, or when the settings
// cannot be read or say nothing, the time given to WithOffers is used.
func WithOfferDurations(source OfferDurationSource) Option {
	return func(s *service) {
		if source == nil {
			panic("offer duration source is required")
		}

		s.offerDurations = source
	}
}

// offerTTLFor is the time a driver gets to accept an offer made now.
func (s *service) offerTTLFor(ctx context.Context, tripID string) time.Duration {
	if s.offerDurations == nil {
		return s.offerTTL
	}

	duration, err := s.offerDurations.OfferDuration(ctx)
	if err != nil {
		s.log().WarnContext(ctx, "dispatch attempt: offer time setting unavailable, using the instance's",
			"trip_id", tripID,
			"error", err,
		)

		return s.offerTTL
	}

	if duration < minOfferDuration || duration > maxOfferDuration {
		return s.offerTTL
	}

	return duration
}
