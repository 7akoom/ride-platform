package trip

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	// DefaultOfferTTL is how long a driver has to answer when the caller does not say.
	DefaultOfferTTL = 15 * time.Second

	// MinOfferTTL and MaxOfferTTL bound what a caller may ask for; a value outside
	// is served as the nearest bound rather than refused.
	MinOfferTTL = 5 * time.Second
	MaxOfferTTL = 60 * time.Second
)

var (
	// ErrTripNotOfferable means the trip is no longer waiting for a driver.
	ErrTripNotOfferable = errors.New("the trip is not waiting for a driver")

	// ErrOfferInProgress means another driver has a live offer for this trip. The
	// caller should wait for it to be answered or to expire.
	ErrOfferInProgress = errors.New("another driver is being offered this trip")

	// ErrAlreadyOffered means this driver was offered this trip before (and did not
	// take it). A driver is never offered the same trip twice.
	ErrAlreadyOffered = errors.New("the driver was already offered this trip")

	// ErrDriverHasPendingOffer means the driver has a live offer for another trip.
	ErrDriverHasPendingOffer = errors.New("the driver already has a pending offer")

	// ErrOfferNotFound means there is no live offer of this trip to this driver: it
	// was never made, was rejected, was withdrawn with the trip, or was accepted.
	ErrOfferNotFound = errors.New("no pending offer")

	// ErrOfferExpired means the driver answered too late.
	ErrOfferExpired = errors.New("the offer has expired")
)

// Offer is a trip put to one driver, to be accepted before ExpiresAt. Trip is set
// when the offer is read back for the driver, and carries what they need to decide.
type Offer struct {
	TripID    string
	DriverID  string
	OfferedAt time.Time
	ExpiresAt time.Time
	Trip      Trip
}

// OfferStore is the outbound port behind driver offers. Every method is atomic:
// the rules "one live offer per trip", "one live offer per driver", "never twice
// to the same driver" and "the driver has no active trip" hold under concurrency.
type OfferStore interface {
	// CreateOffer offers the trip to the driver for ttl. It returns
	// ErrTripNotFound, ErrTripNotOfferable, ErrAlreadyOffered, ErrOfferInProgress,
	// ErrDriverHasPendingOffer or ErrDriverHasActiveTrip when it cannot.
	CreateOffer(ctx context.Context, tripID string, driverID string, ttl time.Duration) (Offer, error)

	// FindPendingOffer returns the driver's live offer, or ErrOfferNotFound.
	FindPendingOffer(ctx context.Context, driverID string) (Offer, error)

	// AcceptOffer accepts the driver's live offer of the trip: the offer, the trip
	// (requested to accepted, with its trip.accepted event) change together or not
	// at all. ErrOfferNotFound, ErrOfferExpired, ErrDriverHasActiveTrip and
	// ErrInvalidTransition (the trip was cancelled meanwhile) are the refusals.
	AcceptOffer(ctx context.Context, tripID string, driverID string) (Trip, error)

	// RejectOffer declines the driver's live offer of the trip, or ErrOfferNotFound.
	RejectOffer(ctx context.Context, tripID string, driverID string) error
}

// WithTripOffers decorates a Service with the driver-offer calls: OfferTrip
// (dispatch puts a trip to a driver), GetPendingOffer (the driver app asks what it
// has been offered), AcceptOffer and RejectOffer (the driver answers). Every other
// method is the base service's. The transport layer reaches them through trip.As,
// so the Service interface (and every fake of it) stays unchanged.
func WithTripOffers(base Service, store OfferStore) Service {
	if base == nil {
		panic("trip service is required")
	}

	if store == nil {
		panic("offer store is required")
	}

	return &offerService{Service: base, store: store}
}

type offerService struct {
	Service

	store OfferStore
}

// Unwrap returns the Service the offer decorator wraps.
func (s *offerService) Unwrap() Service { return s.Service }

// OfferTrip puts the trip to the driver for ttl, clamped to [MinOfferTTL, MaxOfferTTL]
// (DefaultOfferTTL when ttl is zero).
func (s *offerService) OfferTrip(ctx context.Context, tripID string, driverID string, ttl time.Duration) (Offer, error) {
	tripID, driverID, err := offerIDs(tripID, driverID)
	if err != nil {
		return Offer{}, err
	}

	return s.store.CreateOffer(ctx, tripID, driverID, clampOfferTTL(ttl))
}

// GetPendingOffer returns what the driver has been offered and can still take.
func (s *offerService) GetPendingOffer(ctx context.Context, driverID string) (Offer, error) {
	if strings.TrimSpace(driverID) == "" {
		return Offer{}, ErrDriverIDRequired
	}

	return s.store.FindPendingOffer(ctx, strings.TrimSpace(driverID))
}

// AcceptOffer makes the driver the driver of the trip.
func (s *offerService) AcceptOffer(ctx context.Context, tripID string, driverID string) (Trip, error) {
	tripID, driverID, err := offerIDs(tripID, driverID)
	if err != nil {
		return Trip{}, err
	}

	return s.store.AcceptOffer(ctx, tripID, driverID)
}

// RejectOffer declines the offer; the trip goes on to the next driver.
func (s *offerService) RejectOffer(ctx context.Context, tripID string, driverID string) error {
	tripID, driverID, err := offerIDs(tripID, driverID)
	if err != nil {
		return err
	}

	return s.store.RejectOffer(ctx, tripID, driverID)
}

func offerIDs(tripID string, driverID string) (string, string, error) {
	tripID, driverID = strings.TrimSpace(tripID), strings.TrimSpace(driverID)

	switch {
	case tripID == "":
		return "", "", ErrTripIDRequired
	case driverID == "":
		return "", "", ErrDriverIDRequired
	}

	return tripID, driverID, nil
}

func clampOfferTTL(ttl time.Duration) time.Duration {
	switch {
	case ttl <= 0:
		return DefaultOfferTTL
	case ttl < MinOfferTTL:
		return MinOfferTTL
	case ttl > MaxOfferTTL:
		return MaxOfferTTL
	default:
		return ttl
	}
}
