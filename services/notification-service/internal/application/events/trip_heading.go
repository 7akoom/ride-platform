package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// SubjectDriverHeading is published by trip-service when a driver who took a
// trip while finishing another one ends that one and sets off for its pickup.
const SubjectDriverHeading = "trip.driver_heading"

// TripHeadingSubjects are read by the notification-trip-heading consumer.
var TripHeadingSubjects = []string{SubjectDriverHeading}

type driverHeadingPayload struct {
	TripID   string `json:"trip_id"`
	RiderID  string `json:"rider_id"`
	DriverID string `json:"driver_id"`
}

// handleDriverHeading tells the rider their driver has finished the trip before
// theirs and is now on the way.
func (h *Handler) handleDriverHeading(ctx context.Context, envelope Envelope) error {
	var payload driverHeadingPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.driver_heading payload: %w", err)
	}

	if payload.RiderID == "" || payload.DriverID == "" {
		h.logger.WarnContext(ctx, "trip.driver_heading without a rider or a driver; skipping", "trip_id", payload.TripID)

		return nil
	}

	driver, err := h.driverClient.GetDriver(ctx, payload.DriverID)
	if err != nil {
		return fmt.Errorf("look up driver %s: %w", payload.DriverID, err)
	}

	if _, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   payload.RiderID,
		EventKey:      SubjectDriverHeading,
		Variables: map[string]string{
			"driver_name":   driver.DisplayName,
			"vehicle_color": driver.VehicleColor,
			"vehicle_model": driver.VehicleModel,
			"plate_number":  driver.PlateNumber,
		},
		Data:           map[string]string{"trip_id": payload.TripID},
		IdempotencyKey: envelope.EventID,
	}); err != nil {
		return fmt.Errorf("send trip.driver_heading notification: %w", err)
	}

	return nil
}
