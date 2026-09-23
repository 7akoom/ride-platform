package pricing

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// etaSpeedKmh is the speed assumed for a driver's way to the pickup when
// OSRM cannot be reached (city traffic).
const etaSpeedKmh = 25.0

// etaTimeout bounds the routing call for each class's nearest driver: a
// slow OSRM falls back to the straight-line estimate rather than holding
// the quote up.
const etaTimeout = 2 * time.Second

func (s *service) QuoteTrip(ctx context.Context, input QuoteTripInput) (TripQuotes, error) {
	request := fareRequest{
		RiderID:    input.RiderID,
		PickupLat:  input.PickupLat,
		PickupLng:  input.PickupLng,
		DropoffLat: input.DropoffLat,
		DropoffLng: input.DropoffLng,
		CouponCode: input.CouponCode,
	}

	riderID, err := request.validate()
	if err != nil {
		return TripQuotes{}, err
	}

	m, err := s.gatherMarket(ctx, request, riderID)
	if err != nil {
		return TripQuotes{}, err
	}

	quotes := make([]Quote, 0, len(VehicleClasses()))

	for _, class := range VehicleClasses() {
		priced, err := s.priceClass(ctx, m, class)
		if err != nil {
			return TripQuotes{}, fmt.Errorf("price %s: %w", class, err)
		}

		available, eta := s.pickupETA(ctx, m, class)

		quotes = append(quotes, Quote{
			RiderID:          riderID,
			ZoneID:           m.zone.ZoneID,
			CityID:           m.zone.CityID,
			VehicleClass:     class,
			Pickup:           Point{Latitude: request.PickupLat, Longitude: request.PickupLng},
			Dropoff:          Point{Latitude: request.DropoffLat, Longitude: request.DropoffLng},
			Breakdown:        priced.breakdown,
			ConfigID:         priced.config.ID,
			Coupon:           priced.coupon,
			DriversAvailable: available,
			PickupETAMinutes: eta,
			CreatedAt:        m.now,
			ExpiresAt:        m.now.Add(s.quoteTTL),
		})
	}

	sort.SliceStable(quotes, func(i, j int) bool {
		return quotes[i].Breakdown.Total.LessThan(quotes[j].Breakdown.Total)
	})

	saved, err := s.repository.SaveQuotes(ctx, quotes)
	if err != nil {
		return TripQuotes{}, fmt.Errorf("save quotes: %w", err)
	}

	return TripQuotes{ZoneID: m.zone.ZoneID, CityID: m.zone.CityID, Quotes: saved}, nil
}

// pickupETA says whether a free driver of the class is near the pickup and
// how many minutes away by road the nearest one is (at least 1; 0 when
// there is none or the drivers could not be looked up).
func (s *service) pickupETA(ctx context.Context, m market, class string) (bool, int) {
	if !m.driversKnown {
		return false, 0
	}

	for _, driver := range m.drivers {
		if effectiveClass(driver.VehicleClass) != class {
			continue
		}

		routeCtx, cancel := context.WithTimeout(ctx, etaTimeout)
		route, err := s.routingClient.Route(routeCtx,
			driver.Location.Latitude, driver.Location.Longitude,
			m.request.PickupLat, m.request.PickupLng)
		cancel()

		minutes := route.DurationMinutes
		if err != nil {
			km := haversineDistanceKm(driver.Location.Latitude, driver.Location.Longitude,
				m.request.PickupLat, m.request.PickupLng) * 1.3
			minutes = km / etaSpeedKmh * 60
		}

		return true, max(1, int(math.Ceil(minutes)))
	}

	return false, 0
}

func (s *service) ClaimQuote(ctx context.Context, quoteID, riderID, tripID string) (Quote, error) {
	quoteID = strings.TrimSpace(quoteID)
	riderID = strings.TrimSpace(riderID)
	tripID = strings.TrimSpace(tripID)

	switch {
	case quoteID == "":
		return Quote{}, ErrQuoteIDRequired
	case riderID == "":
		return Quote{}, ErrRiderIDRequired
	case tripID == "":
		return Quote{}, ErrTripIDRequired
	case !looksLikeUUID(quoteID), !looksLikeUUID(riderID):
		return Quote{}, ErrQuoteNotFound
	case !looksLikeUUID(tripID):
		return Quote{}, ErrQuoteNotForTrip
	}

	quote, err := s.repository.FindQuote(ctx, quoteID)
	if err != nil {
		return Quote{}, fmt.Errorf("read quote: %w", err)
	}

	if quote.RiderID != riderID {
		return Quote{}, ErrQuoteNotFound
	}

	// The coupon the price used may have ended or run out since: the rider
	// must not get a discount that is no longer on offer.
	if quote.Coupon != nil && quote.ClaimedTripID != tripID {
		if err := s.couponStillUsable(ctx, quote.Coupon.CouponID, riderID); err != nil {
			return Quote{}, err
		}
	}

	claimed, err := s.repository.ClaimQuote(ctx, quoteID, riderID, tripID, nowFunc())
	if err != nil {
		return Quote{}, fmt.Errorf("claim quote: %w", err)
	}

	return claimed, nil
}

func (s *service) couponStillUsable(ctx context.Context, couponID, riderID string) error {
	coupon, err := s.repository.FindCouponByID(ctx, couponID)
	if err != nil {
		return fmt.Errorf("read the quote's coupon: %w", err)
	}

	if !coupon.IsCurrentlyValid(nowFunc()) {
		return ErrQuoteCouponUnavailable
	}

	uses, err := s.repository.RiderRedemptionCount(ctx, couponID, riderID)
	if err != nil {
		return fmt.Errorf("count the rider's coupon uses: %w", err)
	}

	if uses >= coupon.PerRiderLimit {
		return ErrQuoteCouponUnavailable
	}

	return nil
}

func (s *service) ReleaseQuote(ctx context.Context, quoteID, tripID string) error {
	quoteID = strings.TrimSpace(quoteID)
	tripID = strings.TrimSpace(tripID)

	if quoteID == "" {
		return ErrQuoteIDRequired
	}

	if tripID == "" {
		return ErrTripIDRequired
	}

	if !looksLikeUUID(quoteID) || !looksLikeUUID(tripID) {
		// Nothing can hold it: there is nothing to free.
		return nil
	}

	if err := s.repository.ReleaseQuote(ctx, quoteID, tripID); err != nil {
		return fmt.Errorf("release quote: %w", err)
	}

	return nil
}

func (s *service) DeleteUnclaimedQuotes(ctx context.Context) (int, error) {
	return s.repository.DeleteUnclaimedQuotes(ctx, nowFunc().Add(-unclaimedQuoteRetention))
}
