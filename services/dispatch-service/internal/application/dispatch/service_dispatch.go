package dispatch

import (
	"context"
	"fmt"
	"strings"
)

const statusRequested = "requested"
const driverStatusActive = "active"
const driverAvailable = "available"

func (s *service) DispatchTrip(
	ctx context.Context,
	tripID string,
	searchRadiusMeters float64,
) (Result, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return Result{}, ErrTripIDRequired
	}

	radius := searchRadiusMeters
	if radius <= 0 {
		radius = DefaultSearchRadiusMeters
	}

	tripInfo, err := s.tripClient.GetTrip(ctx, trimmedID)
	if err != nil {
		return Result{}, fmt.Errorf("get trip: %w", err)
	}

	if tripInfo.Status != statusRequested {
		return Result{}, ErrTripNotDispatchable
	}

	candidates, err := s.locationClient.FindNearbyDrivers(
		ctx,
		tripInfo.PickupLat,
		tripInfo.PickupLng,
		radius,
		candidateLimit,
	)
	if err != nil {
		return Result{}, fmt.Errorf("find nearby drivers: %w", err)
	}

	if len(candidates) == 0 {
		return Result{}, ErrNoDriversNearby
	}

	// Try candidates nearest-first. A candidate is skipped (not a hard
	// failure) if it's no longer eligible or loses a race to be
	// assigned — the next-nearest candidate is tried instead. Only if
	// every candidate is exhausted do we report failure.
	for _, candidate := range candidates {
		driverInfo, err := s.driverClient.GetDriver(ctx, candidate.DriverID)
		if err != nil {
			continue
		}

		if driverInfo.Status != driverStatusActive || driverInfo.AvailabilityStatus != driverAvailable {
			continue
		}

		// A comfort trip is only offered to comfort drivers, and economy to
		// economy: strict match, no cross-class fallback.
		if effectiveVehicleClass(driverInfo.VehicleClass) != effectiveVehicleClass(tripInfo.VehicleClass) {
			continue
		}

		standing, err := s.walletClient.CheckDriverStanding(ctx, candidate.DriverID)
		if err != nil {
			// Can't verify standing — skip rather than risk assigning a
			// trip to a driver who may be suspended.
			continue
		}

		if !standing.CanTakeTrips {
			continue
		}

		if err := s.tripClient.AcceptTrip(ctx, trimmedID, driverInfo.ID); err != nil {
			// Someone else (a concurrent dispatch, or a manual accept)
			// may have taken this trip or this driver already — move on.
			continue
		}

		// Best-effort: the trip is already accepted at this point, which
		// is the outcome that matters. If marking the driver busy fails,
		// that's a data-consistency issue to notice and fix, but it
		// shouldn't undo a successful dispatch.
		_ = s.driverClient.MarkBusy(ctx, driverInfo.ID)

		return Result{
			TripID:         trimmedID,
			DriverID:       driverInfo.ID,
			DistanceMeters: candidate.DistanceMeters,
		}, nil
	}

	return Result{}, ErrNoDriversAvailable
}
