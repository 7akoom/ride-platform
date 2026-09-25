package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
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
		Stops:      input.Stops,
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

	var card Config

	if quoteID := strings.TrimSpace(input.QuoteID); quoteID != "" {
		persist, card, err = s.quotedFare(ctx, tripID, quoteID)
	} else {
		persist, card, err = s.meteredFare(ctx, tripID, input)
	}

	if err != nil {
		return Fare{}, err
	}

	persist.Kind = FareKindTrip
	addWaiting(&persist.Breakdown, card, input.ArrivedAt, input.StartedAt, s.fareRoundingIncrement)

	persisted, err := s.repository.PersistFare(ctx, persist)
	if errors.Is(err, ErrCouponUnavailable) {
		// The trip held no use of its coupon (quoted before coupons were
		// reserved, or priced now from a code) and none is left: it is
		// charged without the coupon rather than never charged.
		dropCoupon(&persist, card, input, s.fareRoundingIncrement)
		persisted, err = s.repository.PersistFare(ctx, persist)
	}
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

// dropCoupon takes the coupon's discount off a fare about to be recorded
// (no other discount replaces it: the price was the coupon's).
func dropCoupon(persist *PersistFareInput, card Config, input CalculateFareInput, increment decimal.Decimal) {
	b := &persist.Breakdown
	b.AppliedDiscountType = DiscountNone
	b.AppliedDiscountLabel = ""
	b.DiscountAmount = decimal.Zero
	b.CouponStatus = CouponStatusUsedUp
	b.WaitingMinutes = 0
	b.Total = roundToIncrement(b.Subtotal.Add(b.SurgeAmount), increment)
	addWaiting(b, card, input.ArrivedAt, input.StartedAt, increment)

	persist.Coupon = nil
}

// quotedFare is the fare of a trip requested with a quote: exactly what
// the rider was quoted, with the coupon the quote used.
func (s *service) quotedFare(ctx context.Context, tripID, quoteID string) (PersistFareInput, Config, error) {
	if !looksLikeUUID(quoteID) {
		return PersistFareInput{}, Config{}, ErrQuoteNotFound
	}

	quote, err := s.repository.FindQuote(ctx, quoteID)
	if err != nil {
		return PersistFareInput{}, Config{}, fmt.Errorf("read quote: %w", err)
	}

	if quote.ClaimedTripID != tripID {
		return PersistFareInput{}, Config{}, ErrQuoteNotForTrip
	}

	// The waiting fee comes from the card the trip was quoted with.
	var card Config
	if quote.ConfigID != "" {
		if card, err = s.repository.GetConfigByID(ctx, quote.ConfigID); err != nil {
			return PersistFareInput{}, Config{}, fmt.Errorf("read the quote's rate card: %w", err)
		}
	}

	return PersistFareInput{
		TripID:    tripID,
		RiderID:   quote.RiderID,
		Breakdown: quote.Breakdown,
		Coupon:    quote.Coupon,
		QuoteID:   quote.ID,
		ConfigID:  quote.ConfigID,
	}, card, nil
}

// meteredFare prices a trip requested without a quote, now that it is
// over, from its planned pickup and dropoff.
func (s *service) meteredFare(ctx context.Context, tripID string, input CalculateFareInput) (PersistFareInput, Config, error) {
	priced, err := s.buildFare(ctx, fareRequest{
		RiderID:    input.RiderID,
		PickupLat:  input.PickupLat,
		PickupLng:  input.PickupLng,
		DropoffLat: input.DropoffLat,
		DropoffLng: input.DropoffLng,
		CouponCode: input.CouponCode,
		Stops:      input.Stops,
	}, input.VehicleClass)
	if err != nil {
		return PersistFareInput{}, Config{}, err
	}

	return PersistFareInput{
		TripID:    tripID,
		RiderID:   strings.TrimSpace(input.RiderID),
		Breakdown: priced.breakdown,
		Coupon:    priced.coupon,
		ConfigID:  priced.config.ID,
	}, priced.config, nil
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
