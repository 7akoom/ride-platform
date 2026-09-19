package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// handleTripSOSTriggered does two independent things for one SOS:
//
//  1. alerts the operating company out of band, so a human hears about it
//     (this is the part that matters for safety), and
//  2. sends a confirmation to whoever pressed the button.
//
// The operator alert goes first and does not depend on the trip lookup
// succeeding: if trip-service is down, operators still get an alert built
// from the event alone. The alert is delivered once per event even if the
// confirmation fails and the event is redelivered.
func (h *Handler) handleTripSOSTriggered(ctx context.Context, envelope Envelope) error {
	var payload tripSOSTriggeredPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.sos_triggered payload: %w", err)
	}

	trip, tripErr := h.tripClient.GetTrip(ctx, payload.TripID)
	if tripErr != nil {
		h.logger.WarnContext(ctx, "could not look up the trip for an SOS; alerting operators with what the event carries",
			"trip_id", payload.TripID,
			"alert_id", payload.AlertID,
			"error", tripErr,
		)

		trip = TripInfo{ID: payload.TripID}
	}

	// Best-effort enrichment: a missing driver name must never delay an alert.
	driverName := ""

	if tripErr == nil && trip.DriverID != "" {
		driver, err := h.driverClient.GetDriver(ctx, trip.DriverID)
		if err != nil {
			h.logger.WarnContext(ctx, "could not look up the driver for an SOS alert",
				"driver_id", trip.DriverID,
				"error", err,
			)
		} else {
			driverName = driver.DisplayName
		}
	}

	alert := buildSOSAlert(payload, trip, driverName, envelope.OccurredAt)

	if err := h.sos.deliver(ctx, envelope.EventID, alert); err != nil {
		return err
	}

	if tripErr != nil {
		return fmt.Errorf("look up trip %s: %w", payload.TripID, tripErr)
	}

	var (
		recipientType notification.RecipientType
		recipientID   string
	)

	switch payload.TriggeredBy {
	case "rider":
		recipientType = notification.RecipientRider
		recipientID = trip.RiderID
	case "driver":
		recipientType = notification.RecipientDriver
		recipientID = trip.DriverID
	default:
		h.logger.WarnContext(ctx, "unknown sos triggered_by; skipping",
			"triggered_by", payload.TriggeredBy, "alert_id", payload.AlertID)

		return nil
	}

	if recipientID == "" {
		h.logger.WarnContext(ctx, "sos alert has no resolvable recipient; skipping",
			"triggered_by", payload.TriggeredBy, "trip_id", payload.TripID)

		return nil
	}

	_, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType:  recipientType,
		RecipientID:    recipientID,
		EventKey:       "trip.sos_confirmed",
		Variables:      map[string]string{},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send trip.sos_confirmed notification: %w", err)
	}

	return nil
}
