package trip

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	// ErrPreviousTripNotFinished: the driver took this trip while on another one
	// and has not finished that one yet, so they cannot be at this pickup.
	ErrPreviousTripNotFinished = errors.New("finish your current trip first")

	// ErrNotOfferableAfterTrip: a trip can be offered to a driver to take after
	// their current one only while that one is theirs and under way (the rider in
	// the car), and they have no other trip waiting.
	ErrNotOfferableAfterTrip = errors.New("the driver cannot take another trip after this one")
)

// MaxOfferPickupETA bounds the road time an offer carries; anything above is a
// mistake and is not shown.
const MaxOfferPickupETA = 2 * time.Hour

// WaitingForPreviousTrip: the driver took this trip while finishing another one
// and has not set off for its pickup. It stays true on a trip cancelled before
// they did, which is what tells that cancelling it cost the rider nothing.
func (t Trip) WaitingForPreviousTrip() bool {
	return t.AfterTripID != "" && t.HeadingAt == nil
}

// OfferChain says how a trip is offered: AfterTripID is the driver's current
// trip when they are to take this one after it, and PickupETA the road time to
// the pickup dispatch reckoned (zero when not known).
type OfferChain struct {
	AfterTripID string
	PickupETA   time.Duration
}

// ChainedOfferStore is the offer store that can offer a trip to a driver to take
// after their current one, and carry the road time to the pickup.
type ChainedOfferStore interface {
	CreateChainedOffer(ctx context.Context, tripID, driverID string, ttl time.Duration, chain OfferChain) (Offer, error)
}

// NextTripStore finds the trip a driver took to do after their current one.
type NextTripStore interface {
	FindNextByDriverID(ctx context.Context, driverID string) (Trip, error)
}

// OfferTripWithChain is OfferTrip with how the trip is offered. Without a store
// that can chain offers, an offer after another trip is refused.
func (s *offerService) OfferTripWithChain(
	ctx context.Context,
	tripID string,
	driverID string,
	ttl time.Duration,
	chain OfferChain,
) (Offer, error) {
	tripID, driverID, err := offerIDs(tripID, driverID)
	if err != nil {
		return Offer{}, err
	}

	chain.AfterTripID = strings.TrimSpace(chain.AfterTripID)
	if chain.PickupETA < 0 || chain.PickupETA > MaxOfferPickupETA {
		chain.PickupETA = 0
	}

	chained, ok := s.store.(ChainedOfferStore)
	if !ok {
		if chain.AfterTripID != "" {
			return Offer{}, ErrNotOfferableAfterTrip
		}

		return s.store.CreateOffer(ctx, tripID, driverID, clampOfferTTL(ttl))
	}

	if chain.AfterTripID != "" && (chain.AfterTripID == tripID || !looksLikeUUID(chain.AfterTripID)) {
		return Offer{}, ErrNotOfferableAfterTrip
	}

	return chained.CreateChainedOffer(ctx, tripID, driverID, clampOfferTTL(ttl), chain)
}

// NextTrip is the trip the driver took to do after their current one, or
// ErrTripNotFound when there is none.
func (s *historyService) NextTrip(ctx context.Context, driverID string) (Trip, error) {
	driverID = strings.TrimSpace(driverID)
	if driverID == "" {
		return Trip{}, ErrDriverIDRequired
	}

	store, ok := s.store.(NextTripStore)
	if !ok {
		return Trip{}, ErrTripNotFound
	}

	return store.FindNextByDriverID(ctx, driverID)
}
