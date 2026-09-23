package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
	"github.com/shopspring/decimal"
)

// Handler turns domain events published by other services into
// notifications. It is the Dispatch method that gets wired as the
// nats.MessageHandler for every durable consumer this service runs.
//
// Only a subset of the events each publisher emits has a matching
// seeded template today (see migrations/00005_seed_notification_templates.sql):
// trip.accepted, trip.started, trip.cancelled and fare.calculated. Other
// events (trip.requested, trip.settled, driver.* ...) are deliberately
// left unhandled for now — Dispatch acks them without action so they
// don't get stuck redelivering.
type Handler struct {
	notificationService notification.Service
	tripClient          TripClient
	driverClient        DriverClient
	logger              *slog.Logger
	sos                 *sosDelivery
}

// Option customises a Handler at construction time.
type Option func(*Handler)

// WithOperatorAlerter makes an SOS reach a human at the operating company,
// not only the person who pressed the button. A failed alert is retried
// every retryInterval until giveUpAfter has passed since the SOS was
// raised, after which an error is logged and it stops.
func WithOperatorAlerter(alerter OperatorAlerter, retryInterval, giveUpAfter time.Duration) Option {
	return func(h *Handler) {
		if alerter == nil {
			return
		}

		if retryInterval <= 0 {
			panic("SOS retry interval must be positive")
		}

		if giveUpAfter <= 0 {
			panic("SOS give-up window must be positive")
		}

		h.sos.alerter = alerter
		h.sos.retryInterval = retryInterval
		h.sos.giveUpAfter = giveUpAfter
	}
}

func NewHandler(
	notificationService notification.Service,
	tripClient TripClient,
	driverClient DriverClient,
	logger *slog.Logger,
	options ...Option,
) *Handler {
	if notificationService == nil {
		panic("notification service is required")
	}

	if tripClient == nil {
		panic("trip client is required")
	}

	if driverClient == nil {
		panic("driver client is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	handler := &Handler{
		notificationService: notificationService,
		tripClient:          tripClient,
		driverClient:        driverClient,
		logger:              logger,
		sos:                 newSOSDelivery(logger),
	}

	for _, option := range options {
		option(handler)
	}

	return handler
}

// Dispatch decodes the envelope and routes it to the matching handler by
// subject. Unrecognised subjects are logged and acked (returning nil)
// rather than treated as an error — there is nothing to retry toward.
func (h *Handler) Dispatch(ctx context.Context, subject string, data []byte) error {
	envelope, err := Decode(data)
	if err != nil {
		return fmt.Errorf("decode event envelope: %w", err)
	}

	switch subject {
	case "trip.accepted":
		return h.handleTripAccepted(ctx, envelope)
	case "trip.started":
		return h.handleTripStarted(ctx, envelope)
	case "trip.cancelled":
		return h.handleTripCancelled(ctx, envelope)
	case "fare.calculated":
		return h.handleFareCalculated(ctx, envelope)
	case "trip.sos_triggered":
		return h.handleTripSOSTriggered(ctx, envelope)
	case "trip.offered":
		return h.handleTripOffered(ctx, envelope)
	case "trip.driver_arrived":
		return h.handleDriverArrived(ctx, envelope)
	default:
		h.logger.WarnContext(ctx, "no notification mapping for subject; skipping", "subject", subject)

		return nil
	}
}

type tripAcceptedPayload struct {
	TripID   string `json:"trip_id"`
	DriverID string `json:"driver_id"`
}

func (h *Handler) handleTripAccepted(ctx context.Context, envelope Envelope) error {
	var payload tripAcceptedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.accepted payload: %w", err)
	}

	trip, err := h.tripClient.GetTrip(ctx, payload.TripID)
	if err != nil {
		return fmt.Errorf("look up trip %s: %w", payload.TripID, err)
	}

	driver, err := h.driverClient.GetDriver(ctx, payload.DriverID)
	if err != nil {
		return fmt.Errorf("look up driver %s: %w", payload.DriverID, err)
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   trip.RiderID,
		EventKey:      "trip.driver_assigned",
		Variables: map[string]string{
			"driver_name":   driver.DisplayName,
			"vehicle_color": driver.VehicleColor,
			"vehicle_model": driver.VehicleModel,
			"plate_number":  driver.PlateNumber,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send trip.driver_assigned notification: %w", err)
	}

	return nil
}

type tripStartedPayload struct {
	TripID string `json:"trip_id"`
}

func (h *Handler) handleTripStarted(ctx context.Context, envelope Envelope) error {
	var payload tripStartedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.started payload: %w", err)
	}

	trip, err := h.tripClient.GetTrip(ctx, payload.TripID)
	if err != nil {
		return fmt.Errorf("look up trip %s: %w", payload.TripID, err)
	}

	// No reverse-geocoding exists yet, so the dropoff renders as raw
	// coordinates rather than an address. Revisit once Location exposes one.
	dropoff := fmt.Sprintf("%.5f, %.5f", trip.DropoffLatitude, trip.DropoffLongitude)

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   trip.RiderID,
		EventKey:      "trip.started",
		Variables: map[string]string{
			"dropoff": dropoff,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send trip.started notification: %w", err)
	}

	return nil
}

type tripCancelledPayload struct {
	TripID string `json:"trip_id"`
	Reason string `json:"reason"`
}

func (h *Handler) handleTripCancelled(ctx context.Context, envelope Envelope) error {
	var payload tripCancelledPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.cancelled payload: %w", err)
	}

	trip, err := h.tripClient.GetTrip(ctx, payload.TripID)
	if err != nil {
		return fmt.Errorf("look up trip %s: %w", payload.TripID, err)
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   trip.RiderID,
		EventKey:      "trip.cancelled",
		Variables: map[string]string{
			"reason": payload.Reason,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send trip.cancelled notification: %w", err)
	}

	return nil
}

// Total crosses as a decimal string, never a float — matching the
// "money crosses the wire as a string" convention pricing-service and
// wallet-service both follow, so this consumer can't quietly lose the
// precision the publisher went out of its way to preserve.
type fareCalculatedPayload struct {
	TripID       string `json:"trip_id"`
	RiderID      string `json:"rider_id"`
	CurrencyCode string `json:"currency_code"`
	Total        string `json:"total"`
	// Kind is trip, or cancellation / no_show for a cancelled trip's fee.
	Kind string `json:"kind"`
}

// handleFareCalculated fires the "trip.completed" notification. It
// subscribes to fare.calculated rather than a trip.completed event
// because trip-service's own trip.completed payload carries no amount —
// fare.calculated (published by pricing-service right after) is the
// first event that has rider_id, total and currency together, and the
// template needs exactly those.
func (h *Handler) handleFareCalculated(ctx context.Context, envelope Envelope) error {
	var payload fareCalculatedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode fare.calculated payload: %w", err)
	}

	total, err := decimal.NewFromString(payload.Total)
	if err != nil {
		return fmt.Errorf("parse fare.calculated total %q: %w", payload.Total, err)
	}

	eventKey := "trip.completed"

	switch payload.Kind {
	case "cancellation":
		eventKey = "trip.cancellation_fee"
	case "no_show":
		eventKey = "trip.no_show_fee"
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   payload.RiderID,
		EventKey:      eventKey,
		Variables: map[string]string{
			"total":    total.StringFixed(2),
			"currency": payload.CurrencyCode,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send %s notification: %w", eventKey, err)
	}

	return nil
}

type driverArrivedPayload struct {
	TripID   string `json:"trip_id"`
	RiderID  string `json:"rider_id"`
	DriverID string `json:"driver_id"`
}

// handleDriverArrived tells the rider their driver is at the pickup.
func (h *Handler) handleDriverArrived(ctx context.Context, envelope Envelope) error {
	var payload driverArrivedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.driver_arrived payload: %w", err)
	}

	riderID := payload.RiderID
	if riderID == "" {
		trip, err := h.tripClient.GetTrip(ctx, payload.TripID)
		if err != nil {
			return fmt.Errorf("look up trip %s: %w", payload.TripID, err)
		}

		riderID = trip.RiderID
	}

	driver, err := h.driverClient.GetDriver(ctx, payload.DriverID)
	if err != nil {
		return fmt.Errorf("look up driver %s: %w", payload.DriverID, err)
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   riderID,
		EventKey:      "trip.driver_arrived",
		Variables: map[string]string{
			"driver_name":   driver.DisplayName,
			"vehicle_color": driver.VehicleColor,
			"vehicle_model": driver.VehicleModel,
			"plate_number":  driver.PlateNumber,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send trip.driver_arrived notification: %w", err)
	}

	return nil
}
