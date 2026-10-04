package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// SubjectScheduleFailed is published by trip-service when a ride booked ahead
// could not be turned into a trip (no driver, or the pickup no longer served).
const SubjectScheduleFailed = "trip.schedule_failed"

// TripScheduleSubjects are read by the notification-trip-schedules consumer.
var TripScheduleSubjects = []string{SubjectScheduleFailed}

type scheduleFailedPayload struct {
	ScheduledTripID string `json:"scheduled_trip_id"`
	RiderID         string `json:"rider_id"`
}

// handleScheduleFailed tells the rider their booked ride will not come, so they
// can request one now. The reason is for staff (it may be an internal error),
// not for the rider.
func (h *Handler) handleScheduleFailed(ctx context.Context, envelope Envelope) error {
	var payload scheduleFailedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode trip.schedule_failed payload: %w", err)
	}

	if payload.RiderID == "" {
		h.logger.WarnContext(ctx, "trip.schedule_failed without a rider; skipping", "scheduled_trip_id", payload.ScheduledTripID)

		return nil
	}

	if _, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType:  notification.RecipientRider,
		RecipientID:    payload.RiderID,
		EventKey:       "trip.schedule_failed",
		Variables:      map[string]string{},
		Data:           map[string]string{"scheduled_trip_id": payload.ScheduledTripID},
		IdempotencyKey: envelope.EventID,
	}); err != nil {
		return fmt.Errorf("send trip.schedule_failed notification: %w", err)
	}

	return nil
}
