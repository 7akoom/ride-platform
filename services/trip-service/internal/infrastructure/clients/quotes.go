package clients

import (
	"context"
	"fmt"
	"time"

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

	var stops []trip.Coordinates
	for _, stop := range response.GetStops() {
		stops = append(stops, trip.Coordinates{Latitude: stop.GetLatitude(), Longitude: stop.GetLongitude()})
	}

	return trip.ClaimedQuote{
		Stops:        stops,
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

// NoShowWait is how long the driver of the trip waits at the pickup before they
// may report a no-show: the rate card's (pricing-service). See trip.NoShowWaits.
func (b *QuoteBook) NoShowWait(ctx context.Context, tripID, riderID string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := b.pricing.GetTripTerms(ctx, &pricingv1.GetTripTermsRequest{RiderId: riderID, TripId: tripID})
	if err != nil {
		return 0, fmt.Errorf("pricing-service GetTripTerms: %w", err)
	}

	return time.Duration(response.GetNoShowWaitMinutes()) * time.Minute, nil
}

// RateTerms is what the trip's rate card says about waiting and no-shows
// (pricing-service, asked with the internal token). See trip.RateTermsReader.
func (b *QuoteBook) RateTerms(ctx context.Context, tripID, riderID string) (trip.RateTerms, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := b.pricing.GetTripTerms(ctx, &pricingv1.GetTripTermsRequest{RiderId: riderID, TripId: tripID})
	if status.Code(err) == codes.NotFound {
		return trip.RateTerms{}, trip.ErrDriverTermsUnavailable
	}

	if err != nil {
		return trip.RateTerms{}, fmt.Errorf("pricing-service GetTripTerms: %w", err)
	}

	return trip.RateTerms{
		CurrencyCode:       response.GetCurrencyCode(),
		FreeWaitingMinutes: int(response.GetFreeWaitingMinutes()),
		WaitingPerMinute:   response.GetWaitingPerMinute(),
		NoShowWaitMinutes:  int(response.GetNoShowWaitMinutes()),
		NoShowFee:          response.GetNoShowFee(),
	}, nil
}
