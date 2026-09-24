package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// SubjectTripCompleted is the JetStream subject trip-service's outbox
// publishes when a trip is completed.
const SubjectTripCompleted = "trip.completed"

// SubjectTripCancelled is published when a trip is cancelled; a cancelled
// trip may owe a cancellation or no-show fee.
const SubjectTripCancelled = "trip.cancelled"

// TripInfo is the slice of a trip pricing needs to calculate its fare.
type TripInfo struct {
	ID           string
	RiderID      string
	PickupLat    float64
	PickupLng    float64
	DropoffLat   float64
	DropoffLng   float64
	VehicleClass string
	// QuoteID is the quote the trip was requested with, if any: its fare
	// is the quoted one.
	QuoteID string

	DriverID    string
	Status      string
	CancelledBy string
	RiderNoShow bool
	AcceptedAt  *time.Time
	ArrivedAt   *time.Time
	StartedAt   *time.Time
	CancelledAt *time.Time
}

// TripReader is pricing-service's view of trip-service.
type TripReader interface {
	GetTrip(ctx context.Context, tripID string) (TripInfo, error)
}

// Envelope mirrors the JSON shape every outbox publisher wraps its
// message in (see each service's infrastructure/messaging/nats
// jetstream_publisher.go).
type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int16           `json:"schema_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func Decode(data []byte) (Envelope, error) {
	var envelope Envelope

	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, err
	}

	return envelope, nil
}

// retryLaterError tells the JetStream consumer to redeliver the message
// after a delay instead of immediately, so a transient failure doesn't
// turn into a hot redelivery loop.
type retryLaterError struct {
	delay time.Duration
	cause error
}

func (e *retryLaterError) Error() string {
	return fmt.Sprintf("retry in %s: %v", e.delay, e.cause)
}

func (e *retryLaterError) Unwrap() error {
	return e.cause
}

// RetryDelay is discovered by the NATS consumer through an interface
// check, so the infrastructure layer never imports this package.
func (e *retryLaterError) RetryDelay() time.Duration {
	return e.delay
}

// Handler turns trip.completed events into fare calculations.
type Handler struct {
	pricer        pricing.Service
	trips         TripReader
	retryInterval time.Duration
	giveUpAfter   time.Duration
	now           func() time.Time
	logger        *slog.Logger
}

func NewHandler(
	pricer pricing.Service,
	trips TripReader,
	retryInterval time.Duration,
	giveUpAfter time.Duration,
	logger *slog.Logger,
) *Handler {
	if pricer == nil {
		panic("pricing service is required")
	}

	if trips == nil {
		panic("trip reader is required")
	}

	if retryInterval <= 0 {
		panic("retry interval must be positive")
	}

	if giveUpAfter <= 0 {
		panic("give-up window must be positive")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &Handler{
		pricer:        pricer,
		trips:         trips,
		retryInterval: retryInterval,
		giveUpAfter:   giveUpAfter,
		now:           time.Now,
		logger:        logger,
	}
}

// Handle processes one JetStream message. Return values follow the
// consumer contract: nil acks (done, or nothing more worth doing), a
// retryLaterError redelivers after a delay.
//
// CalculateFare is idempotent per trip (a fare already on file is
// returned untouched), so redelivery can never double-price a trip or
// publish a second fare.calculated event.
func (h *Handler) Handle(ctx context.Context, subject string, data []byte) error {
	if subject != SubjectTripCompleted && subject != SubjectTripCancelled {
		return nil
	}

	envelope, err := Decode(data)
	if err != nil {
		// A message we can't parse will never parse — ack it so it
		// doesn't redeliver forever.
		h.logger.ErrorContext(ctx, "dropping undecodable trip.completed event", "error", err)

		return nil
	}

	tripID := tripIDFrom(envelope)
	if tripID == "" {
		h.logger.ErrorContext(ctx, "dropping trip.completed event without a trip id", "event_id", envelope.EventID)

		return nil
	}

	if !envelope.OccurredAt.IsZero() && h.now().Sub(envelope.OccurredAt) > h.giveUpAfter {
		// This trip was completed but never priced — that is a real
		// problem worth a human's attention, so it is logged as an error.
		h.logger.ErrorContext(ctx, "giving up calculating fare for completed trip",
			"trip_id", tripID,
			"completed_at", envelope.OccurredAt,
		)

		return nil
	}

	trip, err := h.trips.GetTrip(ctx, tripID)
	if err != nil {
		return h.retryLater(ctx, tripID, fmt.Errorf("get trip: %w", err))
	}

	if subject == SubjectTripCancelled {
		return h.chargeCancellation(ctx, tripID, trip)
	}

	_, err = h.pricer.CalculateFare(ctx, pricing.CalculateFareInput{
		TripID:       tripID,
		RiderID:      trip.RiderID,
		PickupLat:    trip.PickupLat,
		PickupLng:    trip.PickupLng,
		DropoffLat:   trip.DropoffLat,
		DropoffLng:   trip.DropoffLng,
		VehicleClass: trip.VehicleClass,
		QuoteID:      trip.QuoteID,
		ArrivedAt:    trip.ArrivedAt,
		StartedAt:    trip.StartedAt,
	})
	if err == nil {
		h.logger.InfoContext(ctx, "fare calculated for completed trip", "trip_id", tripID)

		return nil
	}

	if isPermanent(err) {
		// Retrying can never fix bad input for this trip.
		h.logger.ErrorContext(ctx, "cannot calculate fare for completed trip",
			"trip_id", tripID,
			"error", err,
		)

		return nil
	}

	return h.retryLater(ctx, tripID, err)
}

// chargeCancellation records a cancelled trip's fee, if it owes one.
func (h *Handler) chargeCancellation(ctx context.Context, tripID string, trip TripInfo) error {
	if trip.Status != "cancelled" {
		h.logger.WarnContext(ctx, "trip.cancelled for a trip that is not cancelled", "trip_id", tripID)

		return nil
	}

	// A cancelled trip never uses the coupon its quote reserved: the use is
	// freed for the rider (and everyone else) again.
	if err := h.pricer.ReleaseTripCoupon(ctx, tripID); err != nil {
		return h.retryLater(ctx, tripID, err)
	}

	fare, charged, err := h.pricer.ChargeCancellation(ctx, pricing.CancellationInput{
		TripID:       tripID,
		RiderID:      trip.RiderID,
		DriverID:     trip.DriverID,
		VehicleClass: trip.VehicleClass,
		QuoteID:      trip.QuoteID,
		PickupLat:    trip.PickupLat,
		PickupLng:    trip.PickupLng,
		CancelledBy:  trip.CancelledBy,
		RiderNoShow:  trip.RiderNoShow,
		AcceptedAt:   trip.AcceptedAt,
		ArrivedAt:    trip.ArrivedAt,
		CancelledAt:  trip.CancelledAt,
	})
	if err == nil {
		if charged {
			h.logger.InfoContext(ctx, "fee charged for cancelled trip", "trip_id", tripID, "kind", string(fare.Kind))
		}

		return nil
	}

	if isPermanent(err) {
		h.logger.ErrorContext(ctx, "cannot charge a fee for cancelled trip", "trip_id", tripID, "error", err)

		return nil
	}

	return h.retryLater(ctx, tripID, err)
}

func (h *Handler) retryLater(ctx context.Context, tripID string, cause error) error {
	h.logger.WarnContext(ctx, "fare calculation failed; will retry",
		"trip_id", tripID,
		"retry_in", h.retryInterval,
		"error", cause,
	)

	return &retryLaterError{delay: h.retryInterval, cause: cause}
}

// isPermanent reports failures that are a property of the trip's own
// data, so trying again would only fail the same way.
func isPermanent(err error) bool {
	return errors.Is(err, pricing.ErrPickupOutsideServiceZone) ||
		errors.Is(err, pricing.ErrInvalidLatitude) ||
		errors.Is(err, pricing.ErrInvalidLongitude) ||
		errors.Is(err, pricing.ErrRiderIDRequired) ||
		errors.Is(err, pricing.ErrTripIDRequired) ||
		errors.Is(err, pricing.ErrInvalidVehicleClass) ||
		errors.Is(err, pricing.ErrQuoteNotFound) ||
		errors.Is(err, pricing.ErrQuoteNotForTrip)
}

// tripIDFrom prefers the envelope's aggregate id (the trip itself) and
// falls back to a trip_id field in the payload.
func tripIDFrom(envelope Envelope) string {
	if id := strings.TrimSpace(envelope.AggregateID); id != "" {
		return id
	}

	var payload struct {
		TripID string `json:"trip_id"`
	}

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return ""
	}

	return strings.TrimSpace(payload.TripID)
}
