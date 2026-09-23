package grpc

import (
	"context"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
)

const pricingRPCPrefix = "/ride.pricing.v1.PricingService/"

// EstimateFare and QuoteTrip are the RPCs an end user may call, and only
// for their own rider profile: a price applies the rider's coupon
// eligibility and redemption history, so asking on behalf of another rider
// would leak both (and a quote is the rider's to request a trip with).
var ownerChecks = map[string]ownerCheck{
	pricingRPCPrefix + "QuoteTrip": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*pricingv1.QuoteTripRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	pricingRPCPrefix + "EstimateFare": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*pricingv1.EstimateFareRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
}
