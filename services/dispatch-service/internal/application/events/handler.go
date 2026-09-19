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

// Handler turns trip.requested events into dispatch attempts.
type Handler struct {
	dispatcher    dispatch.Service
	retryInterval time.Duration
	searchTimeout time.Duration
	now           func() time.Time
	logger        *slog.Logger
}

func NewHandler(
	dispatcher dispatch.Service,
	retryInterval time.Duration,
	searchTimeout time.Duration,
	logger *slog.Logger,
) *Handler {
	if dispatcher == nil {
		panic("dispatcher is required")
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

	// Stale-event guard: past the search window we stop looking. This also
	// protects a freshly created durable consumer, which replays the
	// stream's history from the start, from dispatching long-abandoned
	// trips that were never cancelled.
	if !envelope.OccurredAt.IsZero() && h.now().Sub(envelope.OccurredAt) > h.searchTimeout {
		h.logger.InfoContext(ctx, "trip.requested is past the search window; no longer dispatching",
			"trip_id", tripID,
			"requested_at", envelope.OccurredAt,
		)

		return nil
	}

	result, err := h.dispatcher.DispatchTrip(ctx, tripID, 0)
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
