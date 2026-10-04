package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// SubjectDataExportReady: identity-service made a person's "Download your
// data" file.
const SubjectDataExportReady = "identity.data_export_ready"

// IdentitySubjects are the identity events that notify someone.
var IdentitySubjects = []string{SubjectDataExportReady}

type dataExportPayload struct {
	RiderID   string    `json:"rider_id"`
	DriverID  string    `json:"driver_id"`
	ExportID  string    `json:"export_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleDataExportReady tells the person on their rider app, or their driver
// app when they have no rider profile.
func (h *Handler) handleDataExportReady(ctx context.Context, envelope Envelope) error {
	var payload dataExportPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", SubjectDataExportReady, err)
	}

	recipientType, recipientID := notification.RecipientRider, payload.RiderID
	if recipientID == "" {
		recipientType, recipientID = notification.RecipientDriver, payload.DriverID
	}

	if recipientID == "" || payload.ExportID == "" {
		h.logger.InfoContext(ctx, "data export ready for someone with no profile to tell; skipping")

		return nil
	}

	if _, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType:  recipientType,
		RecipientID:    recipientID,
		EventKey:       "account.data_export_ready",
		Variables:      map[string]string{"date": payload.ExpiresAt.UTC().Format("2006-01-02")},
		Data:           map[string]string{"data_export_id": payload.ExportID},
		IdempotencyKey: envelope.EventID,
	}); err != nil {
		return fmt.Errorf("send data export notification: %w", err)
	}

	return nil
}
