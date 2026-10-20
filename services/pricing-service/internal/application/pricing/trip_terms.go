package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// DefaultNoShowWaitMinutes is a card's no-show wait when staff set none.
	DefaultNoShowWaitMinutes = 6
	MaxNoShowWaitMinutes     = 30
)

var (
	// ErrTripTermsNotFound: no such trip, or not this rider's (not told apart).
	ErrTripTermsNotFound = errors.New("trip not found")
	// ErrTripTermsUnavailable: the service was built without a trip reader.
	ErrTripTermsUnavailable = errors.New("trip terms are not configured")
)

// NoShowWaitOrDefault is minutes, or the default when not set.
func NoShowWaitOrDefault(minutes int) int {
	if minutes <= 0 {
		return DefaultNoShowWaitMinutes
	}

	return minutes
}

// TripSnapshot is what the terms of a trip need from trip-service.
type TripSnapshot struct {
	RiderID      string
	DriverID     string
	QuoteID      string
	VehicleClass string
	PickupLat    float64
	PickupLng    float64
	AcceptedAt   *time.Time
	ArrivedAt    *time.Time
}

// TripReader reads a trip (trip-service); ErrTripTermsNotFound when there is none.
type TripReader interface {
	TripSnapshot(ctx context.Context, tripID string) (TripSnapshot, error)
}

// WithTripReader lets the service answer TripTerms.
func WithTripReader(reader TripReader) Option {
	return func(s *service) {
		s.tripReader = reader
	}
}

// TripTerms is what waiting and cancelling cost on one trip, by the card it was
// priced with, and what applies at the moment asked.
type TripTerms struct {
	CurrencyCode             string
	FreeWaitingMinutes       int
	WaitingPerMinute         decimal.Decimal
	CancellationFee          decimal.Decimal
	CancellationGraceMinutes int
	// CancellationFeeFrom: from when cancelling costs the fee; nil before a
	// driver accepted.
	CancellationFeeFrom *time.Time
	// CancellationFeeNow is what cancelling at the moment asked costs: zero in
	// the grace minutes, before a driver accepted, or when the driver is late.
	CancellationFeeNow decimal.Decimal
	NoShowWaitMinutes  int
	NoShowFee          decimal.Decimal
	// NoShowFrom: from when the driver may report that the rider did not come;
	// nil before the driver arrived.
	NoShowFrom *time.Time
}

// TripTerms reads the trip and its card. riderID must be the trip's rider.
func (s *service) TripTerms(ctx context.Context, riderID, tripID string, now time.Time) (TripTerms, error) {
	riderID, tripID = strings.TrimSpace(riderID), strings.TrimSpace(tripID)

	switch {
	case tripID == "":
		return TripTerms{}, ErrTripIDRequired
	case riderID == "":
		return TripTerms{}, ErrRiderIDRequired
	case s.tripReader == nil:
		return TripTerms{}, ErrTripTermsUnavailable
	}

	trip, err := s.tripReader.TripSnapshot(ctx, tripID)
	if err != nil {
		return TripTerms{}, err
	}

	if trip.RiderID != riderID {
		return TripTerms{}, ErrTripTermsNotFound
	}

	card, _, err := s.cancellationCard(ctx, tripID, CancellationInput{
		TripID:       tripID,
		QuoteID:      trip.QuoteID,
		VehicleClass: trip.VehicleClass,
		PickupLat:    trip.PickupLat,
		PickupLng:    trip.PickupLng,
	})
	if err != nil {
		return TripTerms{}, fmt.Errorf("read the trip's rate card: %w", err)
	}

	terms := TripTerms{
		CurrencyCode:             card.CurrencyCode,
		FreeWaitingMinutes:       card.FreeWaitingMinutes,
		WaitingPerMinute:         card.WaitingPerMinute,
		CancellationFee:          roundToIncrement(card.CancellationFee, s.fareRoundingIncrement),
		CancellationGraceMinutes: card.CancellationGraceMinutes,
		CancellationFeeNow:       decimal.Zero,
		NoShowWaitMinutes:        NoShowWaitOrDefault(card.NoShowWaitMinutes),
		NoShowFee:                roundToIncrement(card.NoShowFee, s.fareRoundingIncrement),
	}

	if trip.AcceptedAt != nil {
		from := trip.AcceptedAt.Add(time.Duration(card.CancellationGraceMinutes) * time.Minute)
		terms.CancellationFeeFrom = &from

		kind, owes := cancellationKind(CancellationInput{
			DriverID:    trip.DriverID,
			CancelledBy: "rider",
			AcceptedAt:  trip.AcceptedAt,
			ArrivedAt:   trip.ArrivedAt,
			CancelledAt: &now,
		})
		if owes && kind == FareKindCancellation && !now.Before(from) && terms.CancellationFee.IsPositive() {
			terms.CancellationFeeNow = terms.CancellationFee
		}
	}

	if trip.ArrivedAt != nil {
		from := trip.ArrivedAt.Add(time.Duration(terms.NoShowWaitMinutes) * time.Minute)
		terms.NoShowFrom = &from
	}

	return terms, nil
}
