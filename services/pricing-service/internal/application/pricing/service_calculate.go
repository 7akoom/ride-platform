package pricing

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) EstimateFare(
	ctx context.Context,
	input EstimateFareInput,
) (FareBreakdown, error) {
	breakdown, _, err := s.buildFare(ctx, fareRequest{
		RiderID:    input.RiderID,
		PickupLat:  input.PickupLat,
		PickupLng:  input.PickupLng,
		DropoffLat: input.DropoffLat,
		DropoffLng: input.DropoffLng,
		CouponCode: input.CouponCode,
	})
	if err != nil {
		return FareBreakdown{}, err
	}

	// Note: an estimate deliberately persists nothing — no fare row, no
	// coupon redemption, no rider-stat increment. Riders may check a
	// price several times without committing to a trip.
	return breakdown, nil
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

	breakdown, appliedCoupon, err := s.buildFare(ctx, fareRequest{
		RiderID:    input.RiderID,
		PickupLat:  input.PickupLat,
		PickupLng:  input.PickupLng,
		DropoffLat: input.DropoffLat,
		DropoffLng: input.DropoffLng,
		CouponCode: input.CouponCode,
	})
	if err != nil {
		return Fare{}, err
	}

	persisted, err := s.repository.PersistFare(ctx, PersistFareInput{
		TripID:    tripID,
		RiderID:   strings.TrimSpace(input.RiderID),
		Breakdown: breakdown,
		Coupon:    appliedCoupon,
	})
	if err != nil {
		return Fare{}, fmt.Errorf("persist fare: %w", err)
	}

	return persisted, nil
}
