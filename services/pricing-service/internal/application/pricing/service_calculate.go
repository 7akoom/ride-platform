package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) EstimateFare(
	ctx context.Context,
	input EstimateFareInput,
) (FareBreakdown, error) {
	priced, err := s.buildFare(ctx, fareRequest{
		RiderID:    input.RiderID,
		PickupLat:  input.PickupLat,
		PickupLng:  input.PickupLng,
		DropoffLat: input.DropoffLat,
		DropoffLng: input.DropoffLng,
		CouponCode: input.CouponCode,
	}, input.VehicleClass)
	if err != nil {
		return FareBreakdown{}, err
	}

	// Note: an estimate deliberately persists nothing — no fare row, no
	// coupon redemption, no rider-stat increment. Riders may check a
	// price several times without committing to a trip.
	return priced.breakdown, nil
}

func (s *service) CalculateFare(
	ctx context.Context,
	input CalculateFareInput,
) (Fare, error) {
	tripID := strings.TrimSpace(input.TripID)
	if tripID == "" {
		return Fare{}, ErrTripIDRequired
	}

	// Idempotency: a trip's price is fixed once calculated. A retried
	// call returns the original fare rather than re-running surge and
	// discount logic against different current conditions.
	existing, found, err := s.repository.FindFareByTripID(ctx, tripID)
	if err != nil {
		return Fare{}, fmt.Errorf("look up existing fare: %w", err)
	}

	if found {
		return existing, nil
	}

	var persist PersistFareInput

	if quoteID := strings.TrimSpace(input.QuoteID); quoteID != "" {
		persist, err = s.quotedFare(ctx, tripID, quoteID)
	} else {
		persist, err = s.meteredFare(ctx, tripID, input)
	}

	if err != nil {
		return Fare{}, err
	}

	persisted, err := s.repository.PersistFare(ctx, persist)
	if errors.Is(err, ErrFareAlreadyRecorded) {
		// A concurrent call recorded it first; fares never change, so
		// its fare is this one.
		existing, found, readErr := s.repository.FindFareByTripID(ctx, tripID)
		if readErr != nil {
			return Fare{}, fmt.Errorf("read the fare recorded concurrently: %w", readErr)
		}

		if found {
			return existing, nil
		}
	}

	if err != nil {
		return Fare{}, fmt.Errorf("persist fare: %w", err)
	}

	return persisted, nil
}

// quotedFare is the fare of a trip requested with a quote: exactly what
// the rider was quoted, with the coupon the quote used.
func (s *service) quotedFare(ctx context.Context, tripID, quoteID string) (PersistFareInput, error) {
	if !looksLikeUUID(quoteID) {
		return PersistFareInput{}, ErrQuoteNotFound
	}

	quote, err := s.repository.FindQuote(ctx, quoteID)
	if err != nil {
		return PersistFareInput{}, fmt.Errorf("read quote: %w", err)
	}

	if quote.ClaimedTripID != tripID {
		return PersistFareInput{}, ErrQuoteNotForTrip
	}

	return PersistFareInput{
		TripID:    tripID,
		RiderID:   quote.RiderID,
		Breakdown: quote.Breakdown,
		Coupon:    quote.Coupon,
		QuoteID:   quote.ID,
		ConfigID:  quote.ConfigID,
	}, nil
}

// meteredFare prices a trip requested without a quote, now that it is
// over, from its planned pickup and dropoff.
func (s *service) meteredFare(ctx context.Context, tripID string, input CalculateFareInput) (PersistFareInput, error) {
	priced, err := s.buildFare(ctx, fareRequest{
		RiderID:    input.RiderID,
		PickupLat:  input.PickupLat,
		PickupLng:  input.PickupLng,
		DropoffLat: input.DropoffLat,
		DropoffLng: input.DropoffLng,
		CouponCode: input.CouponCode,
	}, input.VehicleClass)
	if err != nil {
		return PersistFareInput{}, err
	}

	return PersistFareInput{
		TripID:    tripID,
		RiderID:   strings.TrimSpace(input.RiderID),
		Breakdown: priced.breakdown,
		Coupon:    priced.coupon,
		ConfigID:  priced.config.ID,
	}, nil
}

// looksLikeUUID is a cheap shape check so a malformed id is "not found"
// rather than a database cast error.
func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}

	return true
}
