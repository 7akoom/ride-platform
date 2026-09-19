package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/shopspring/decimal"
)

// SubjectFareCalculated is the JetStream subject pricing-service's outbox
// publishes once a completed trip has been priced.
const SubjectFareCalculated = "fare.calculated"

// TripInfo is the slice of a trip settlement needs. fare.calculated
// carries the rider and the total, but not the driver or how the rider
// pays — those only live on the trip.
type TripInfo struct {
	ID       string
	RiderID  string
	DriverID string

	// PaymentMethod is how the rider chose to pay (cash or wallet). Empty
	// for a trip that predates the field, which falls back to the
	// configured default.
	PaymentMethod string
}

// TripReader is wallet-service's view of trip-service.
type TripReader interface {
	GetTrip(ctx context.Context, tripID string) (TripInfo, error)
}

// Envelope mirrors the JSON shape every outbox publisher wraps its
// message in (see each service's infrastructure/messaging/nats
// jetstream_publisher.go). For fare.calculated the aggregate is the fare
// itself, so the trip id lives in the payload, not in AggregateID.
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

// farePayload is the fare.calculated payload. total is decoded through
// decimal.Decimal on purpose: pricing-service marshals it as a bare JSON
// number, while other publishers use quoted strings, and Decimal accepts
// both.
type farePayload struct {
	TripID       string          `json:"trip_id"`
	RiderID      string          `json:"rider_id"`
	CurrencyCode string          `json:"currency_code"`
	Total        decimal.Decimal `json:"total"`
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

// Handler turns fare.calculated events into trip settlements.
type Handler struct {
	settler              wallet.Service
	trips                TripReader
	defaultPaymentMethod wallet.PaymentMethod
	retryInterval        time.Duration
	giveUpAfter          time.Duration
	now                  func() time.Time
	logger               *slog.Logger
}

func NewHandler(
	settler wallet.Service,
	trips TripReader,
	defaultPaymentMethod wallet.PaymentMethod,
	retryInterval time.Duration,
	giveUpAfter time.Duration,
	logger *slog.Logger,
) *Handler {
	if settler == nil {
		panic("wallet service is required")
	}

	if trips == nil {
		panic("trip reader is required")
	}

	if !defaultPaymentMethod.Valid() {
		panic("default payment method must be valid")
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
		settler:              settler,
		trips:                trips,
		defaultPaymentMethod: defaultPaymentMethod,
		retryInterval:        retryInterval,
		giveUpAfter:          giveUpAfter,
		now:                  time.Now,
		logger:               logger,
	}
}

// Handle processes one JetStream message. Return values follow the
// consumer contract: nil acks (done, or nothing more worth doing), a
// retryLaterError redelivers after a delay.
//
// SettleTrip is idempotent per trip (an existing settlement is returned
// untouched), so redelivery can never pay a driver or draw a commission
// twice.
func (h *Handler) Handle(ctx context.Context, subject string, data []byte) error {
	if subject != SubjectFareCalculated {
		return nil
	}

	envelope, err := Decode(data)
	if err != nil {
		// A message we can't parse will never parse — ack it so it
		// doesn't redeliver forever.
		h.logger.ErrorContext(ctx, "dropping undecodable fare.calculated event", "error", err)

		return nil
	}

	var payload farePayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping fare.calculated event with an unreadable payload",
			"event_id", envelope.EventID,
			"error", err,
		)

		return nil
	}

	tripID := strings.TrimSpace(payload.TripID)
	if tripID == "" {
		h.logger.ErrorContext(ctx, "dropping fare.calculated event without a trip id", "event_id", envelope.EventID)

		return nil
	}

	if !envelope.OccurredAt.IsZero() && h.now().Sub(envelope.OccurredAt) > h.giveUpAfter {
		// A priced trip was never settled — that is money that did not
		// move, so it is logged as an error a human should look at.
		h.logger.ErrorContext(ctx, "giving up settling trip",
			"trip_id", tripID,
			"fare_calculated_at", envelope.OccurredAt,
		)

		return nil
	}

	trip, err := h.trips.GetTrip(ctx, tripID)
	if err != nil {
		return h.retryLater(ctx, tripID, fmt.Errorf("get trip: %w", err))
	}

	driverID := strings.TrimSpace(trip.DriverID)
	if driverID == "" {
		// A priced trip with no driver cannot be settled, and retrying
		// will not give it one.
		h.logger.ErrorContext(ctx, "cannot settle trip without a driver", "trip_id", tripID)

		return nil
	}

	riderID := strings.TrimSpace(payload.RiderID)
	if riderID == "" {
		riderID = strings.TrimSpace(trip.RiderID)
	}

	// The method the rider chose when requesting the trip. A trip that
	// predates the field (empty) uses the configured default. Anything
	// unknown is rejected by SettleTrip as a permanent failure.
	paymentMethod := h.defaultPaymentMethod
	if raw := strings.ToLower(strings.TrimSpace(trip.PaymentMethod)); raw != "" {
		paymentMethod = wallet.PaymentMethod(raw)
	}

	_, err = h.settler.SettleTrip(ctx, wallet.SettleTripInput{
		TripID:        tripID,
		RiderID:       riderID,
		DriverID:      driverID,
		PaymentMethod: paymentMethod,
		FareAmount:    payload.Total,
	})
	if err == nil {
		h.logger.InfoContext(ctx, "trip settled",
			"trip_id", tripID,
			"driver_id", driverID,
			"fare_amount", payload.Total.String(),
			"payment_method", string(paymentMethod),
		)

		return nil
	}

	if isPermanent(err) {
		// Retrying can never fix bad input for this trip.
		h.logger.ErrorContext(ctx, "cannot settle trip",
			"trip_id", tripID,
			"error", err,
		)

		return nil
	}

	return h.retryLater(ctx, tripID, err)
}

func (h *Handler) retryLater(ctx context.Context, tripID string, cause error) error {
	h.logger.WarnContext(ctx, "settlement failed; will retry",
		"trip_id", tripID,
		"retry_in", h.retryInterval,
		"error", cause,
	)

	return &retryLaterError{delay: h.retryInterval, cause: cause}
}

// isPermanent reports failures that are a property of the trip's own
// data, so trying again would only fail the same way.
func isPermanent(err error) bool {
	return errors.Is(err, wallet.ErrTripIDRequired) ||
		errors.Is(err, wallet.ErrRiderIDRequired) ||
		errors.Is(err, wallet.ErrDriverIDRequired) ||
		errors.Is(err, wallet.ErrInvalidPaymentMethod) ||
		errors.Is(err, wallet.ErrInvalidFareAmount)
}
