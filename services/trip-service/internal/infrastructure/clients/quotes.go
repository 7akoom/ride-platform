package clients

import (
	"context"
	"fmt"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// QuoteBook claims fare quotes from pricing-service, as a service (the
// connection carries the internal token).
type QuoteBook struct {
	pricing pricingv1.PricingServiceClient
}

var _ trip.QuoteBook = (*QuoteBook)(nil)

func NewQuoteBook(pricingConn grpc.ClientConnInterface) *QuoteBook {
	if pricingConn == nil {
		panic("pricing-service connection is required")
	}

	return &QuoteBook{pricing: pricingv1.NewPricingServiceClient(pricingConn)}
}

func (b *QuoteBook) Claim(ctx context.Context, quoteID, riderID, tripID string) (trip.ClaimedQuote, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := b.pricing.ClaimQuote(ctx, &pricingv1.ClaimQuoteRequest{QuoteId: quoteID, RiderId: riderID, TripId: tripID})

	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound, codes.InvalidArgument:
		return trip.ClaimedQuote{}, trip.ErrQuoteNotFound
	case codes.FailedPrecondition:
		return trip.ClaimedQuote{}, fmt.Errorf("%w: %s", trip.ErrQuoteNotUsable, status.Convert(err).Message())
	default:
		return trip.ClaimedQuote{}, fmt.Errorf("%w: pricing-service ClaimQuote: %v", trip.ErrUpstreamUnavailable, err)
	}

	return trip.ClaimedQuote{
		ID:           response.GetQuoteId(),
		VehicleClass: response.GetVehicleClass(),
		Pickup:       trip.Coordinates{Latitude: response.GetPickup().GetLatitude(), Longitude: response.GetPickup().GetLongitude()},
		Dropoff:      trip.Coordinates{Latitude: response.GetDropoff().GetLatitude(), Longitude: response.GetDropoff().GetLongitude()},
		Total:        response.GetTotal(),
		CurrencyCode: response.GetCurrencyCode(),
	}, nil
}

func (b *QuoteBook) Release(ctx context.Context, quoteID, tripID string) error {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	if _, err := b.pricing.ReleaseQuote(ctx, &pricingv1.ReleaseQuoteRequest{QuoteId: quoteID, TripId: tripID}); err != nil {
		return fmt.Errorf("pricing-service ReleaseQuote: %w", err)
	}

	return nil
}
