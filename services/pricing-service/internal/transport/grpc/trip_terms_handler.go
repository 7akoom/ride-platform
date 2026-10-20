package grpc

import (
	"context"
	"errors"
	"time"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// tripTermsReader is what pricing.WithTripReader adds to the service.
type tripTermsReader interface {
	TripTerms(ctx context.Context, riderID, tripID string, now time.Time) (pricing.TripTerms, error)
}

// GetTripTerms is what waiting and cancelling cost on one of the rider's trips.
// The interceptor has already checked that the rider is the caller.
func (h *PricingHandler) GetTripTerms(
	ctx context.Context,
	request *pricingv1.GetTripTermsRequest,
) (*pricingv1.GetTripTermsResponse, error) {
	reader, ok := h.pricingService.(tripTermsReader)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "trip terms are not configured")
	}

	terms, err := reader.TripTerms(ctx, request.GetRiderId(), request.GetTripId(), time.Now())

	switch {
	case errors.Is(err, pricing.ErrTripTermsNotFound):
		return nil, status.Error(codes.NotFound, err.Error())
	case errors.Is(err, pricing.ErrTripTermsUnavailable):
		return nil, status.Error(codes.Unimplemented, err.Error())
	case err != nil:
		return nil, h.mapPricingError(err)
	}

	response := &pricingv1.GetTripTermsResponse{
		CurrencyCode:             terms.CurrencyCode,
		FreeWaitingMinutes:       int32(terms.FreeWaitingMinutes),
		WaitingPerMinute:         terms.WaitingPerMinute.String(),
		CancellationFee:          terms.CancellationFee.String(),
		CancellationGraceMinutes: int32(terms.CancellationGraceMinutes),
		CancellationFeeNow:       terms.CancellationFeeNow.String(),
		NoShowWaitMinutes:        int32(terms.NoShowWaitMinutes),
		NoShowFee:                terms.NoShowFee.String(),
	}

	if terms.CancellationFeeFrom != nil {
		response.CancellationFeeFrom = timestamppb.New(*terms.CancellationFeeFrom)
	}

	if terms.NoShowFrom != nil {
		response.NoShowFrom = timestamppb.New(*terms.NoShowFrom)
	}

	return response, nil
}
