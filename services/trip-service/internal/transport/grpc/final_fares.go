package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// WithFinalFares lets the trip lists (ListTrips and ListActivityFeed) show what each
// trip cost. Without it the final fare fields stay empty.
func WithFinalFares(fares trip.FinalFares) HandlerOption {
	return func(h *TripHandler) { h.finalFares = fares }
}

// addFinalFares fills the final fare of each trip listed, in one call to pricing. When
// pricing does not answer the list still does, without the fares: the app shows the
// trip without a price rather than no list at all.
func (h *TripHandler) addFinalFares(ctx context.Context, trips []*tripv1.Trip) {
	if h.finalFares == nil || len(trips) == 0 {
		return
	}

	ids := make([]string, 0, len(trips))
	for _, t := range trips {
		ids = append(ids, t.GetId())
	}

	totals, err := h.finalFares.Totals(ctx, ids)
	if err != nil {
		h.logger.Warn("trip list without final fares", "error", err)

		return
	}

	for _, t := range trips {
		if fare, ok := totals[t.GetId()]; ok {
			t.FinalFare = fare.Total
			t.FinalFareKind = fare.Kind
			t.FinalFareCurrencyCode = fare.CurrencyCode
		}
	}
}
