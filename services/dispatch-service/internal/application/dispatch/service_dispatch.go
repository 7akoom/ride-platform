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
		s.log().InfoContext(ctx, "dispatch attempt: no drivers found near the pickup point",
			"trip_id", trimmedID,
			"radius_meters", radius,
		)

		return Result{}, ErrNoDriversNearby
	}

	// Every skipped candidate is recorded with its reason. Skipping is
	// deliberately not an error (the next-nearest candidate is tried), but
	// when NOBODY can be assigned the reasons are the only way to tell a
	// suspended wallet from a stale trip from a wrong vehicle class.
	var skipped []string

	skip := func(driverID string, format string, args ...any) {
		skipped = append(skipped, driverID+": "+fmt.Sprintf(format, args...))
	}

	// Try candidates nearest-first. A candidate is skipped (not a hard
	// failure) if it's no longer eligible or loses a race to be
	// assigned — the next-nearest candidate is tried instead. Only if
	// every candidate is exhausted do we report failure.
	for _, candidate := range candidates {
		driverInfo, err := s.driverClient.GetDriver(ctx, candidate.DriverID)
		if err != nil {
			skip(candidate.DriverID, "could not load driver: %v", err)

			continue
		}

		if driverInfo.Status != driverStatusActive || driverInfo.AvailabilityStatus != driverAvailable {
			skip(candidate.DriverID, "not eligible (status=%s, availability=%s)",
				driverInfo.Status, driverInfo.AvailabilityStatus)

			continue
		}

		// A comfort trip is only offered to comfort drivers, and economy to
		// economy: strict match, no cross-class fallback.
		driverClass := effectiveVehicleClass(driverInfo.VehicleClass)
		tripClass := effectiveVehicleClass(tripInfo.VehicleClass)

		if driverClass != tripClass {
			skip(candidate.DriverID, "vehicle class mismatch (driver=%s, trip=%s)", driverClass, tripClass)

			continue
		}

		standing, err := s.walletClient.CheckDriverStanding(ctx, candidate.DriverID)
		if err != nil {
			// Can't verify standing — skip rather than risk assigning a
			// trip to a driver who may be suspended.
			skip(candidate.DriverID, "could not verify wallet standing: %v", err)

			continue
		}

		if !standing.CanTakeTrips {
			skip(candidate.DriverID, "wallet does not allow trips (suspended=%t, reason=%q)",
				standing.Suspended, standing.Reason)

			continue
		}

		if err := s.tripClient.AcceptTrip(ctx, trimmedID, driverInfo.ID); err != nil {
			// Someone else (a concurrent dispatch, or a manual accept)
			// may have taken this trip or this driver already — move on.
			skip(candidate.DriverID, "accept trip failed: %v", err)

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

	s.log().InfoContext(ctx, "dispatch attempt: nearby drivers found but none could be assigned",
		"trip_id", trimmedID,
		"candidates", len(candidates),
		"skipped", skipped,
	)

	return Result{}, ErrNoDriversAvailable
}
