package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

// The trip events that change whether a driver is on a trip.
const (
	SubjectTripAccepted  = "trip.accepted"
	SubjectTripCompleted = "trip.completed"
	SubjectTripCancelled = "trip.cancelled"

	// LifecycleDurable is this consumer's own durable name on the trip events stream.
	LifecycleDurable = "dispatch-trip-lifecycle"
)

// LifecycleSubjects is what the lifecycle consumer listens to.
var LifecycleSubjects = []string{SubjectTripAccepted, SubjectTripCompleted, SubjectTripCancelled}

const (
	driverAvailabilityAvailable = "available"
	driverAvailabilityBusy      = "busy"

	// lifecycleRetryDelay is the pause before an event that could not be handled is
	// tried again (a service it needs was unreachable).
	lifecycleRetryDelay = 5 * time.Second

	// lifecycleGiveUpAfter is how long past its own time an event that keeps failing
	// is retried, so a permanently failing one cannot retry forever.
	lifecycleGiveUpAfter = 10 * time.Minute
)

// LifecycleTrips is the lifecycle handler's view of trip-service.
type LifecycleTrips interface {
	// DriverOfTrip returns the trip's driver, or "" when it never had one.
	DriverOfTrip(ctx context.Context, tripID string) (string, error)

	// HasActiveTrip reports whether the driver has an accepted or in-progress trip.
	HasActiveTrip(ctx context.Context, driverID string) (bool, error)
}

// LifecycleDrivers is the lifecycle handler's view of driver-service.
type LifecycleDrivers interface {
	GetDriver(ctx context.Context, driverID string) (dispatch.DriverInfo, error)
	MarkBusy(ctx context.Context, driverID string) error
	MarkAvailable(ctx context.Context, driverID string) error
}

// LifecycleHandler keeps a driver's availability in line with their trips: busy
// while they have an accepted or in-progress trip, available again once they do not.
//
// Whoever accepts a trip (dispatch assigning it, or the driver accepting an offer),
// nothing else marks the driver busy or releases them afterwards, so this does.
//
// It never trusts the event for what the driver's state should be: an event only
// says "look at this driver". The handler then reads the truth (does the driver
// have an active trip right now?) and moves the driver only from available to busy
// or from busy to available. That makes it safe to run twice, out of order, or on a
// replay of old history: whatever the event, the outcome is the driver's current
// state. A driver who has gone offline is never touched.
type LifecycleHandler struct {
	trips   LifecycleTrips
	drivers LifecycleDrivers
	now     func() time.Time
	logger  *slog.Logger
}

func NewLifecycleHandler(trips LifecycleTrips, drivers LifecycleDrivers, logger *slog.Logger) *LifecycleHandler {
	if trips == nil {
		panic("lifecycle trips are required")
	}

	if drivers == nil {
		panic("lifecycle drivers are required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &LifecycleHandler{trips: trips, drivers: drivers, now: time.Now, logger: logger}
}

// Handle processes one JetStream message; return values follow the consumer
// contract (nil acks, a retryLaterError redelivers after a delay).
func (h *LifecycleHandler) Handle(ctx context.Context, subject string, data []byte) error {
	switch subject {
	case SubjectTripAccepted, SubjectTripCompleted, SubjectTripCancelled:
	default:
		return nil
	}

	envelope, err := Decode(data)
	if err != nil {
		// A message we can't parse will never parse: ack it.
		h.logger.ErrorContext(ctx, "dropping undecodable trip lifecycle event", "subject", subject, "error", err)

		return nil
	}

	driverID := driverIDFrom(envelope)

	if driverID == "" {
		tripID := tripIDFrom(envelope)
		if tripID == "" {
			h.logger.ErrorContext(ctx, "dropping trip lifecycle event without a trip or a driver", "subject", subject, "event_id", envelope.EventID)

			return nil
		}

		// trip.cancelled does not name the driver; the trip does.
		if driverID, err = h.trips.DriverOfTrip(ctx, tripID); err != nil {
			return h.retry(ctx, envelope, tripID, fmt.Errorf("find the trip's driver: %w", err))
		}

		if driverID == "" {
			return nil
		}
	}

	if err := h.reconcile(ctx, driverID, subject); err != nil {
		return h.retry(ctx, envelope, driverID, err)
	}

	return nil
}

func (h *LifecycleHandler) reconcile(ctx context.Context, driverID string, subject string) error {
	driver, err := h.drivers.GetDriver(ctx, driverID)
	if err != nil {
		return fmt.Errorf("load driver: %w", err)
	}

	active, err := h.trips.HasActiveTrip(ctx, driverID)
	if err != nil {
		return fmt.Errorf("check the driver's active trip: %w", err)
	}

	switch {
	case active && driver.AvailabilityStatus == driverAvailabilityAvailable:
		if err := h.drivers.MarkBusy(ctx, driverID); err != nil {
			return fmt.Errorf("mark driver busy: %w", err)
		}

		h.logger.InfoContext(ctx, "driver is on a trip: marked busy", "driver_id", driverID, "event", subject)

	case !active && driver.AvailabilityStatus == driverAvailabilityBusy:
		if err := h.drivers.MarkAvailable(ctx, driverID); err != nil {
			return fmt.Errorf("mark driver available: %w", err)
		}

		h.logger.InfoContext(ctx, "driver has no trip any more: marked available", "driver_id", driverID, "event", subject)
	}

	return nil
}

func (h *LifecycleHandler) retry(ctx context.Context, envelope Envelope, about string, cause error) error {
	if !envelope.OccurredAt.IsZero() && h.now().Sub(envelope.OccurredAt) > lifecycleGiveUpAfter {
		h.logger.ErrorContext(ctx, "could not update a driver's availability after a trip event; giving up",
			"about", about,
			"event_id", envelope.EventID,
			"error", cause,
		)

		return nil
	}

	h.logger.WarnContext(ctx, "updating a driver's availability failed; will retry",
		"about", about,
		"retry_in", lifecycleRetryDelay,
		"error", cause,
	)

	return &retryLaterError{delay: lifecycleRetryDelay, cause: cause}
}

// driverIDFrom reads driver_id from the event payload (trip.accepted and
// trip.completed carry it).
func driverIDFrom(envelope Envelope) string {
	var payload struct {
		DriverID string `json:"driver_id"`
	}

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return ""
	}

	return strings.TrimSpace(payload.DriverID)
}
