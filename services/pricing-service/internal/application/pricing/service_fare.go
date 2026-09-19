package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// nowFunc is a package-level indirection so tests can freeze time when
// they're added later (surge rules and coupon validity are both
// time-dependent, which is painful to test against the real clock).
var nowFunc = func() time.Time {
	return time.Now().UTC()
}

func validateCoordinates(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return ErrInvalidLatitude
	}

	if lng < -180 || lng > 180 {
		return ErrInvalidLongitude
	}

	return nil
}

// routeOrFallback asks OSRM for a real road route and degrades to a
// Haversine estimate if it can't. Fares are the one thing a rider
// always needs an answer for, so an unreachable routing engine must
// not turn into a failed request — it turns into a slightly less
// accurate price, flagged as Estimated on the Route.
func (s *service) routeOrFallback(
	ctx context.Context,
	config Config,
	pickupLat, pickupLng, dropoffLat, dropoffLng float64,
) Route {
	route, err := s.routingClient.Route(ctx, pickupLat, pickupLng, dropoffLat, dropoffLng)
	if err != nil {
		return fallbackRoute(config, pickupLat, pickupLng, dropoffLat, dropoffLng)
	}

	return route
}

type fareRequest struct {
	RiderID      string
	PickupLat    float64
	PickupLng    float64
	DropoffLat   float64
	DropoffLng   float64
	CouponCode   string
	VehicleClass string
}

// buildFare is the one place the full pricing pipeline lives, shared by
// EstimateFare and CalculateFare so an estimate can never drift from
// what the rider is actually charged: rate card -> distance/duration ->
// surge -> discount -> total.
func (s *service) buildFare(
	ctx context.Context,
	request fareRequest,
) (FareBreakdown, *AppliedCoupon, error) {
	riderID := strings.TrimSpace(request.RiderID)
	if riderID == "" {
		return FareBreakdown{}, nil, ErrRiderIDRequired
	}

	if err := validateCoordinates(request.PickupLat, request.PickupLng); err != nil {
		return FareBreakdown{}, nil, err
	}

	if err := validateCoordinates(request.DropoffLat, request.DropoffLng); err != nil {
		return FareBreakdown{}, nil, err
	}

	vehicleClass, err := NormalizeVehicleClass(request.VehicleClass)
	if err != nil {
		return FareBreakdown{}, nil, err
	}

	served, zoneID, err := s.locationClient.CheckServiceZone(ctx, request.PickupLat, request.PickupLng)
	if err != nil {
		return FareBreakdown{}, nil, fmt.Errorf("check service zone: %w", err)
	}

	if !served {
		return FareBreakdown{}, nil, ErrPickupOutsideServiceZone
	}

	config, err := s.repository.GetActiveConfig(ctx, zoneID, vehicleClass)
	if err != nil {
		return FareBreakdown{}, nil, fmt.Errorf("get active pricing config: %w", err)
	}

	route := s.routeOrFallback(
		ctx,
		config,
		request.PickupLat,
		request.PickupLng,
		request.DropoffLat,
		request.DropoffLng,
	)

	breakdown := baseFareBreakdown(config, route)
	breakdown.ZoneID = zoneID
	breakdown.VehicleClass = vehicleClass

	rules, err := s.repository.ListActiveSurgeTimeRules(ctx)
	if err != nil {
		return FareBreakdown{}, nil, fmt.Errorf("list surge time rules: %w", err)
	}

	breakdown.Surge = s.calculateSurge(
		ctx,
		rules,
		request.PickupLat,
		request.PickupLng,
		nowFunc(),
	)
	breakdown.SurgeAmount = breakdown.Subtotal.Mul(breakdown.Surge.Multiplier.Sub(decimal.NewFromInt(1)))

	// Discounts apply to the surged amount, not the pre-surge subtotal —
	// otherwise a percentage coupon would be worth less exactly when the
	// rider is paying the most.
	chargeable := breakdown.Subtotal.Add(breakdown.SurgeAmount)

	discount, err := s.selectBestDiscount(ctx, riderID, request.CouponCode, chargeable)
	if err != nil {
		// A bad or unknown coupon code shouldn't fail the whole fare —
		// the rider still gets a valid price, just without the discount.
		// Any other error is a real failure and propagates.
		if !errors.Is(err, ErrCouponNotFound) {
			return FareBreakdown{}, nil, fmt.Errorf("select discount: %w", err)
		}

		discount = selectedDiscount{Type: DiscountNone, Amount: decimal.Zero}
	}

	breakdown.AppliedDiscountType = discount.Type
	breakdown.AppliedDiscountLabel = discount.Label
	breakdown.DiscountAmount = discount.Amount

	total := chargeable.Sub(discount.Amount)
	if total.IsNegative() {
		total = decimal.Zero
	}

	breakdown.Total = total

	return breakdown, discount.Coupon, nil
}
