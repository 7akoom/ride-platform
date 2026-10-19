package clients

import (
	"context"
	"fmt"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"google.golang.org/grpc"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// FinalFares reads the recorded totals of trips from pricing-service, as a service
// (the connection carries the internal token).
type FinalFares struct {
	pricing pricingv1.PricingServiceClient
}

var _ trip.FinalFares = (*FinalFares)(nil)

func NewFinalFares(pricingConn grpc.ClientConnInterface) *FinalFares {
	if pricingConn == nil {
		panic("pricing-service connection is required")
	}

	return &FinalFares{pricing: pricingv1.NewPricingServiceClient(pricingConn)}
}

func (f *FinalFares) Totals(ctx context.Context, tripIDs []string) (map[string]trip.FinalFare, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := f.pricing.ListTripFares(ctx, &pricingv1.ListTripFaresRequest{TripIds: tripIDs})
	if err != nil {
		return nil, fmt.Errorf("pricing-service ListTripFares: %w", err)
	}

	totals := make(map[string]trip.FinalFare, len(response.GetFares()))

	for _, fare := range response.GetFares() {
		totals[fare.GetTripId()] = trip.FinalFare{
			Kind:         fare.GetKind(),
			Total:        fare.GetTotal(),
			CurrencyCode: fare.GetCurrencyCode(),
		}
	}

	return totals, nil
}
