package dispatch

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrOfferPending means another driver has a live offer of this trip. Dispatch
	// stops looking for now: the offer will be answered or will expire, and the
	// next attempt goes on from there.
	ErrOfferPending = errors.New("another driver has a live offer for this trip")

	// ErrDriverNotOfferable means the driver cannot be offered this trip: they were
	// offered it before, have another offer pending, or are on a trip. The next
	// candidate is tried.
	ErrDriverNotOfferable = errors.New("the driver cannot be offered this trip")
)

// TripOfferer is what a trip client has to do for dispatch to work by offers: put a
// trip to one driver for a limited time. It returns ErrOfferPending or
// ErrDriverNotOfferable (wrapped) for the two refusals dispatch acts on.
//
// It is a separate interface, asked for through WithOffers, so TripClient and its
// fakes stay as they are.
type TripOfferer interface {
	OfferTrip(ctx context.Context, tripID string, driverID string, ttl time.Duration) error
}

// WithOffers makes dispatch put a trip to the nearest eligible driver, who has ttl
// to accept it, instead of assigning it outright. The trip goes on to the next
// driver when the offer is rejected or expires, and a driver is never offered the
// same trip twice (trip-service remembers, so dispatch keeps no state).
//
// A ttl of zero leaves dispatch as it is: the trip is assigned to the nearest
// eligible driver at once. The trip client must implement TripOfferer.
func WithOffers(ttl time.Duration) Option {
	return func(s *service) {
		if ttl < 0 {
			panic("offer ttl must not be negative")
		}

		if ttl == 0 {
			return
		}

		if _, ok := s.tripClient.(TripOfferer); !ok {
			panic("the trip client cannot make offers")
		}

		s.offerTTL = ttl
	}
}

// offerToCandidate is the offers-mode step of DispatchTrip for one eligible
// candidate. done reports that DispatchTrip is finished: with a Result when the
// offer was made, or with ErrOfferPending when another driver's offer is live.
// When it is false the candidate was skipped (and the reason recorded) and the
// next one is tried.
func (s *service) offerToCandidate(
	ctx context.Context,
	tripID string,
	candidate NearbyDriver,
	driver DriverInfo,
	skip func(driverID string, format string, args ...any),
) (result Result, done bool, err error) {
	offerer, _ := s.tripClient.(TripOfferer)

	offerErr := offerer.OfferTrip(ctx, tripID, driver.ID, s.offerTTL)

	switch {
	case offerErr == nil:
		s.log().InfoContext(ctx, "dispatch attempt: trip offered to a driver",
			"trip_id", tripID,
			"driver_id", driver.ID,
			"distance_meters", candidate.DistanceMeters,
			"ttl", s.offerTTL,
		)

		return Result{
			TripID:         tripID,
			DriverID:       driver.ID,
			DistanceMeters: candidate.DistanceMeters,
			Offered:        true,
		}, true, nil

	case errors.Is(offerErr, ErrOfferPending):
		return Result{}, true, ErrOfferPending

	default:
		skip(candidate.DriverID, "not offered: %v", offerErr)

		return Result{}, false, nil
	}
}
