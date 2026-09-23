package pricing

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// driverLateAfter: a rider who cancels this long after a driver accepted,
// with the driver still not at the pickup, pays nothing — the driver is late.
const driverLateAfter = 15 * time.Minute

// addWaiting charges the whole minutes the driver waited at the pickup
// beyond the card's free ones, on top of the total. The total is rounded
// again so it stays a whole multiple of the increment.
func addWaiting(b *FareBreakdown, card Config, arrivedAt, startedAt *time.Time, increment decimal.Decimal) {
	b.WaitingFare = decimal.Zero

	if arrivedAt == nil || startedAt == nil || !startedAt.After(*arrivedAt) || !card.WaitingPerMinute.IsPositive() {
		return
	}

	minutes := int(math.Floor(startedAt.Sub(*arrivedAt).Minutes())) - card.FreeWaitingMinutes
	if minutes <= 0 {
		return
	}

	b.WaitingMinutes = minutes
	b.WaitingFare = card.WaitingPerMinute.Mul(decimal.NewFromInt(int64(minutes)))
	b.Total = roundToIncrement(b.Total.Add(b.WaitingFare), increment)
}

// cancellationKind decides whether a cancelled trip owes a fee, before any
// rate card is read:
//
//	the driver cancels because the rider did not come   -> no_show
//	the rider cancels once a driver accepted             -> cancellation,
//	    unless within the card's grace minutes, or the driver is late
//	    (not at the pickup driverLateAfter after accepting)
//	anyone else, or before a driver accepted             -> nothing
//
// The grace minutes are the card's, so ChargeCancellation checks them once
// it has the card.
func cancellationKind(in CancellationInput) (FareKind, bool) {
	if strings.TrimSpace(in.DriverID) == "" || in.AcceptedAt == nil || in.CancelledAt == nil {
		return "", false
	}

	switch {
	case in.CancelledBy == "driver" && in.RiderNoShow:
		return FareKindNoShow, true
	case in.CancelledBy != "rider":
		return "", false
	}

	elapsed := in.CancelledAt.Sub(*in.AcceptedAt)
	if in.ArrivedAt == nil && elapsed > driverLateAfter {
		return "", false
	}

	return FareKindCancellation, true
}

func (s *service) ChargeCancellation(ctx context.Context, in CancellationInput) (Fare, bool, error) {
	tripID := strings.TrimSpace(in.TripID)
	if tripID == "" {
		return Fare{}, false, ErrTripIDRequired
	}

	kind, owes := cancellationKind(in)
	if !owes {
		return Fare{}, false, nil
	}

	existing, found, err := s.repository.FindFareByTripID(ctx, tripID)
	if err != nil {
		return Fare{}, false, fmt.Errorf("look up existing fare: %w", err)
	}

	if found {
		return existing, true, nil
	}

	card, zone, err := s.cancellationCard(ctx, tripID, in)
	if err != nil {
		return Fare{}, false, err
	}

	fee := card.NoShowFee

	if kind == FareKindCancellation {
		grace := time.Duration(card.CancellationGraceMinutes) * time.Minute
		if in.CancelledAt.Sub(*in.AcceptedAt) < grace {
			return Fare{}, false, nil
		}

		fee = card.CancellationFee
	}

	if !fee.IsPositive() {
		return Fare{}, false, nil
	}

	breakdown := FareBreakdown{
		VehicleClass: card.VehicleClass,
		CurrencyCode: card.CurrencyCode,
		ZoneID:       zone.ZoneID,
		CityID:       zone.CityID,
		Surge:        SurgeBreakdown{Multiplier: decimal.NewFromInt(1)},
		Total:        roundToIncrement(fee, s.fareRoundingIncrement),
	}

	if class, err := NormalizeVehicleClass(in.VehicleClass); err == nil {
		breakdown.VehicleClass = class
	}

	persisted, err := s.repository.PersistFare(ctx, PersistFareInput{
		TripID:    tripID,
		RiderID:   strings.TrimSpace(in.RiderID),
		Kind:      kind,
		Breakdown: breakdown,
		QuoteID:   strings.TrimSpace(in.QuoteID),
		ConfigID:  card.ID,
	})
	if err != nil {
		existing, found, readErr := s.repository.FindFareByTripID(ctx, tripID)
		if readErr == nil && found {
			return existing, true, nil
		}

		return Fare{}, false, fmt.Errorf("persist cancellation fee: %w", err)
	}

	return persisted, true, nil
}

// cancellationCard is the rate card whose fees apply: the one the trip was
// quoted with, or the one in force at its pickup now.
func (s *service) cancellationCard(ctx context.Context, tripID string, in CancellationInput) (Config, ServiceZone, error) {
	if quoteID := strings.TrimSpace(in.QuoteID); quoteID != "" && looksLikeUUID(quoteID) {
		quote, err := s.repository.FindQuote(ctx, quoteID)
		if err != nil {
			return Config{}, ServiceZone{}, fmt.Errorf("read quote: %w", err)
		}

		if quote.ClaimedTripID != tripID {
			return Config{}, ServiceZone{}, ErrQuoteNotForTrip
		}

		if quote.ConfigID != "" {
			card, err := s.repository.GetConfigByID(ctx, quote.ConfigID)
			if err != nil {
				return Config{}, ServiceZone{}, fmt.Errorf("read the quote's rate card: %w", err)
			}

			return card, ServiceZone{Served: true, ZoneID: quote.ZoneID, CityID: quote.CityID}, nil
		}
	}

	if err := validateCoordinates(in.PickupLat, in.PickupLng); err != nil {
		return Config{}, ServiceZone{}, err
	}

	zone, err := s.locationClient.CheckServiceZone(ctx, in.PickupLat, in.PickupLng)
	if err != nil {
		return Config{}, ServiceZone{}, fmt.Errorf("check service zone: %w", err)
	}

	class, err := NormalizeVehicleClass(in.VehicleClass)
	if err != nil {
		class = VehicleClassEconomy
	}

	scope := Scope{VehicleClass: class}
	if zone.Served {
		scope.ZoneID, scope.CityID = zone.ZoneID, zone.CityID
	}

	card, err := s.repository.GetActiveConfig(ctx, scope)
	if err != nil {
		return Config{}, ServiceZone{}, fmt.Errorf("get active pricing config: %w", err)
	}

	return card, zone, nil
}
