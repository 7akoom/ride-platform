package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

// SubjectTripRequested is the JetStream subject trip-service's outbox
// publishes when a rider asks for a trip.
const SubjectTripRequested = "trip.requested"

const (
	// tripStatusRequested is the only status a trip can be auto-cancelled
	// from: a trip that has already been accepted (or started) has a driver
	// and must never be cancelled by a dispatch timeout.
	tripStatusRequested = "requested"

	// cancelReasonNoDrivers is recorded on the trip and shown to the rider.
	cancelReasonNoDrivers = "no drivers available"

	// cancelRetryWindow is how long past the search window a failing
	// cancellation is retried before an error is logged and it stops, so a
	// permanently failing trip cannot retry forever.
	cancelRetryWindow = 10 * time.Minute
)

// TripCanceller is dispatch-service's view of trip-service for the one
// job of closing out a trip nobody could be found for.
type TripCanceller interface {
	GetTrip(ctx context.Context, tripID string) (dispatch.TripInfo, error)
	CancelTrip(ctx context.Context, tripID string, reason string) error
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
// after a delay instead of immediately. Without it, "no driver available
// right now" would turn into a hot redelivery loop.
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

// Handler turns trip.requested events into dispatch attempts, and closes
// out trips that no driver could be found for.
type Handler struct {
	dispatcher    dispatch.Service
	trips         TripCanceller
	retryInterval time.Duration
	searchTimeout time.Duration
	now           func() time.Time
	logger        *slog.Logger
}

func NewHandler(
	dispatcher dispatch.Service,
	trips TripCanceller,
	retryInterval time.Duration,
	searchTimeout time.Duration,
	logger *slog.Logger,
) *Handler {
	if dispatcher == nil {
		panic("dispatcher is required")
	}

	if trips == nil {
		panic("trip canceller is required")
	}

	if retryInterval <= 0 {
		panic("retry interval must be positive")
	}

	if searchTimeout <= 0 {
		panic("search timeout must be positive")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &Handler{
		dispatcher:    dispatcher,
		trips:         trips,
		retryInterval: retryInterval,
		searchTimeout: searchTimeout,
		now:           time.Now,
		logger:        logger,
	}
}

// Handle processes one JetStream message. Return values follow the
// consumer contract: nil acks (done, or nothing more worth doing), a
// retryLaterError redelivers after a delay, any other error redelivers
// immediately.
func (h *Handler) Handle(ctx context.Context, subject string, data []byte) error {
	if subject != SubjectTripRequested {
		return nil
	}

	envelope, err := Decode(data)
	if err != nil {
		// A message we can't parse will never parse — ack it so it
		// doesn't redeliver forever.
		h.logger.ErrorContext(ctx, "dropping undecodable trip.requested event", "error", err)

		return nil
	}

	tripID := tripIDFrom(envelope)
	if tripID == "" {
		h.logger.ErrorContext(ctx, "dropping trip.requested event without a trip id", "event_id", envelope.EventID)

		return nil
	}

	// Past the search window we stop looking — and, if the trip is still
	// waiting for a driver, we close it out so the rider is not left stuck
	// on a request nobody will ever serve. This also covers a freshly
	// created durable consumer replaying the stream's history.
	if !envelope.OccurredAt.IsZero() && h.now().Sub(envelope.OccurredAt) > h.searchTimeout {
		return h.giveUp(ctx, tripID, envelope.OccurredAt)
	}

	result, err := h.dispatcher.DispatchTrip(ctx, tripID, 0)
	if err == nil && result.Offered {
		return h.awaitOffer(ctx, result)
	}

	if errors.Is(err, dispatch.ErrOfferPending) {
		return h.awaitPendingOffer(ctx, tripID)
	}

	if err == nil {
		h.logger.InfoContext(ctx, "trip dispatched automatically",
			"trip_id", result.TripID,
			"driver_id", result.DriverID,
			"distance_meters", result.DistanceMeters,
		)

		return nil
	}

	if errors.Is(err, dispatch.ErrTripNotDispatchable) {
		// Already accepted (manually or by a redelivery that raced us),
		// cancelled, or finished — nothing left to do.
		h.logger.InfoContext(ctx, "trip is no longer dispatchable; skipping", "trip_id", tripID)

		return nil
	}

	if errors.Is(err, dispatch.ErrNoDriversNearby) || errors.Is(err, dispatch.ErrNoDriversAvailable) {
		h.logger.DebugContext(ctx, "no driver available yet; will retry",
			"trip_id", tripID,
			"retry_in", h.retryInterval,
		)
	} else {
		h.logger.WarnContext(ctx, "dispatch attempt failed; will retry",
			"trip_id", tripID,
			"retry_in", h.retryInterval,
			"error", err,
		)
	}

	return &retryLaterError{delay: h.retryInterval, cause: err}
}

// giveUp runs once the search window has passed. It cancels the trip only
// if the trip is STILL waiting for a driver: one that has been accepted,
// started or finished is left completely alone.
func (h *Handler) giveUp(ctx context.Context, tripID string, requestedAt time.Time) error {
	trip, err := h.trips.GetTrip(ctx, tripID)
	if err != nil {
		return h.retryGiveUp(ctx, tripID, requestedAt, fmt.Errorf("get trip: %w", err))
	}

	if trip.Status != tripStatusRequested {
		h.logger.InfoContext(ctx, "search window passed but the trip is no longer waiting for a driver; nothing to cancel",
			"trip_id", tripID,
			"status", trip.Status,
		)

		return nil
	}

	if err := h.trips.CancelTrip(ctx, tripID, cancelReasonNoDrivers); err != nil {
		return h.retryGiveUp(ctx, tripID, requestedAt, fmt.Errorf("cancel trip: %w", err))
	}

	h.logger.InfoContext(ctx, "cancelled trip: no driver was found within the search window",
		"trip_id", tripID,
		"requested_at", requestedAt,
		"search_timeout", h.searchTimeout,
	)

	return nil
}

// retryGiveUp retries a failed close-out on a timer, up to cancelRetryWindow
// past the search window, and then stops with an error a human should see.
func (h *Handler) retryGiveUp(ctx context.Context, tripID string, requestedAt time.Time, cause error) error {
	if h.now().Sub(requestedAt) > h.searchTimeout+cancelRetryWindow {
		h.logger.ErrorContext(ctx, "could not close out a trip that found no driver; giving up",
			"trip_id", tripID,
			"requested_at", requestedAt,
			"error", cause,
		)

		return nil
	}

	h.logger.WarnContext(ctx, "closing out a trip that found no driver failed; will retry",
		"trip_id", tripID,
		"retry_in", h.retryInterval,
		"error", cause,
	)

	return &retryLaterError{delay: h.retryInterval, cause: cause}
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
