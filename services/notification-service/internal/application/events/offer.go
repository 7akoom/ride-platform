package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// tripOfferedPayload is the trip.offered payload. It names the trip and the driver and says
// when the offer ends; it deliberately says nothing about the rider.
type tripOfferedPayload struct {
	TripID    string `json:"trip_id"`
	DriverID  string `json:"driver_id"`
	ExpiresAt string `json:"expires_at"`
}

// handleTripOffered pushes "you have a trip offer" to the driver it was offered to. An offer is
// only worth a push while it is live, so one that has already expired is dropped instead of
// retried: a driver must never be woken for a trip they can no longer accept.
func (h *Handler) handleTripOffered(ctx context.Context, envelope Envelope) error {
	var payload tripOfferedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		// It will never parse: acking is the only useful thing to do with it.
		h.logger.ErrorContext(ctx, "dropping a trip.offered event with an unreadable payload",
			"event_id", envelope.EventID,
			"error", err,
		)

		return nil
	}

	if payload.TripID == "" || payload.DriverID == "" {
		h.logger.ErrorContext(ctx, "dropping a trip.offered event without a trip or a driver",
			"event_id", envelope.EventID,
		)

		return nil
	}

	if expiresAt, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt); err == nil && !expiresAt.After(time.Now()) {
		h.logger.InfoContext(ctx, "not pushing a trip offer that has already expired",
			"trip_id", payload.TripID,
			"driver_id", payload.DriverID,
		)

		return nil
	}

	_, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientDriver,
		RecipientID:   payload.DriverID,
		EventKey:      "trip.offer_received",
		// What the app needs to open the right screen: which kind of push this is, and which
		// trip. It reads the offer's details (and the rider-free summary) from the API.
		Data: map[string]string{
			"type":       "trip.offer_received",
			"trip_id":    payload.TripID,
			"expires_at": payload.ExpiresAt,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send trip.offer_received notification: %w", err)
	}

	return nil
}
