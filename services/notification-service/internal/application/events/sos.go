package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// tripSOSTriggeredPayload mirrors the outbox payload trip-service writes
// for trip.sos_triggered. Every value crosses as a string (coordinates
// included), and the payload carries no rider_id/driver_id, so the
// recipient is resolved through GetTrip using triggered_by.
type tripSOSTriggeredPayload struct {
	TripID      string `json:"trip_id"`
	AlertID     string `json:"alert_id"`
	TriggeredBy string `json:"triggered_by"`
}

// handleTripSOSTriggered sends a confirmation to whoever pressed the SOS
// button. Alerting an operator is a separate item: there is no operator
// recipient type or real out-of-band channel yet.
func (h *Handler) handleTripSOSTriggered(ctx context.Context, envelope Envelope) error {
	var payload tripSOSTriggeredPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.sos_triggered payload: %w", err)
	}

	trip, err := h.tripClient.GetTrip(ctx, payload.TripID)
	if err != nil {
		return fmt.Errorf("look up trip %s: %w", payload.TripID, err)
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

	_, err = h.notificationService.Send(ctx, notification.SendInput{
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
