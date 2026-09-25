package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const (
	// claimLease is how long a claimed booking is left alone by other
	// schedulers while this one requests its trip.
	claimLease = time.Minute
	claimBatch = 20
	// retryAfter is how long a booking that could not be dispatched waits
	// before the next try.
	retryAfter = 30 * time.Second
)

// Dispatcher turns due bookings into trips.
type Dispatcher struct {
	store    Store
	trips    Trips
	limits   Limits
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

func NewDispatcher(store Store, trips Trips, limits Limits, interval time.Duration, logger *slog.Logger) *Dispatcher {
	switch {
	case store == nil:
		panic("schedule store is required")
	case trips == nil:
		panic("trip service is required")
	case interval <= 0:
		panic("the scheduler interval must be positive")
	case logger == nil:
		panic("logger is required")
	}

	return &Dispatcher{store: store, trips: trips, limits: limits, interval: interval, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Run dispatches due bookings every interval until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		if err := d.Tick(ctx); err != nil && ctx.Err() == nil {
			d.logger.Error("scheduled trips could not be dispatched this round", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Tick dispatches the bookings due now.
func (d *Dispatcher) Tick(ctx context.Context) error {
	now := d.now()

	due, err := d.store.ClaimDue(ctx, now, claimLease, claimBatch)
	if err != nil {
		return fmt.Errorf("claim due scheduled trips: %w", err)
	}

	for _, ride := range due {
		if err := d.dispatch(ctx, ride, now); err != nil {
			d.logger.Error("a scheduled trip could not be settled", "scheduled_trip_id", ride.ID, "error", err)
		}
	}

	return nil
}

func (d *Dispatcher) dispatch(ctx context.Context, ride Ride, now time.Time) error {
	// A trip with the booking's id means an earlier round made it and did
	// not get to record it.
	made, err := d.trips.GetTrip(ctx, ride.ID)
	if err != nil && !errors.Is(err, trip.ErrTripNotFound) {
		return d.retryOrFail(ctx, ride, now, err)
	}

	if err != nil {
		made, err = d.request(ctx, ride)
		if err != nil {
			return d.retryOrFail(ctx, ride, now, err)
		}
	}

	switch err := d.store.MarkDispatched(ctx, ride.ID, made.ID, now); {
	case errors.Is(err, ErrNotScheduled):
		// The rider cancelled while the trip was being requested.
		if _, cancelErr := d.trips.CancelTrip(ctx, trip.CancelInput{
			TripID: made.ID, Reason: "the scheduled trip was cancelled", By: trip.CancelledBySystem,
		}); cancelErr != nil && !errors.Is(cancelErr, trip.ErrInvalidTransition) {
			return fmt.Errorf("cancel the trip of a cancelled booking: %w", cancelErr)
		}

		return nil
	case err != nil:
		return fmt.Errorf("record the dispatched trip: %w", err)
	}

	d.logger.Info("scheduled trip dispatched", "scheduled_trip_id", ride.ID)

	return nil
}

func (d *Dispatcher) request(ctx context.Context, ride Ride) (trip.Trip, error) {
	input := trip.RequestTripInput{
		RiderID:               ride.RiderID,
		PickupLat:             ride.Pickup.Latitude,
		PickupLng:             ride.Pickup.Longitude,
		DropoffLat:            ride.Dropoff.Latitude,
		DropoffLng:            ride.Dropoff.Longitude,
		PickupAddress:         ride.PickupAddress,
		DropoffAddress:        ride.DropoffAddress,
		PickupSavedAddressID:  ride.PickupSavedAddressID,
		DropoffSavedAddressID: ride.DropoffSavedAddressID,
		VehicleClass:          ride.VehicleClass,
		PaymentMethod:         ride.PaymentMethod,
		PassengerName:         ride.PassengerName,
		PassengerPhone:        ride.PassengerPhone,
		ScheduledTripID:       ride.ID,
		Stops:                 ride.Stops,
	}

	made, err := d.trips.RequestTrip(ctx, input)

	// A saved address deleted since the booking: the point and address kept
	// when it was booked will do.
	if errors.Is(err, trip.ErrSavedAddressNotFound) && (input.PickupSavedAddressID != "" || input.DropoffSavedAddressID != "") {
		input.PickupSavedAddressID, input.DropoffSavedAddressID = "", ""
		made, err = d.trips.RequestTrip(ctx, input)
	}

	return made, err
}

func (d *Dispatcher) retryOrFail(ctx context.Context, ride Ride, now time.Time, cause error) error {
	reason := failureReason(cause)

	if !now.Before(ride.ScheduledAt.Add(d.limits.Grace)) {
		if err := d.store.Fail(ctx, ride.ID, reason, now); err != nil && !errors.Is(err, ErrNotScheduled) {
			return fmt.Errorf("give up on the booking: %w", err)
		}

		d.logger.Warn("scheduled trip failed", "scheduled_trip_id", ride.ID, "error", cause)

		return nil
	}

	if err := d.store.Retry(ctx, ride.ID, reason, now.Add(retryAfter)); err != nil && !errors.Is(err, ErrNotScheduled) {
		return fmt.Errorf("record the retry: %w", err)
	}

	return nil
}
