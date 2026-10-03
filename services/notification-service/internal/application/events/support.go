package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// Subjects support-service publishes for the people in a ticket.
const (
	SubjectSupportReplyReceived    = "support.reply_received"
	SubjectSupportTicketResolved   = "support.ticket_resolved"
	SubjectSupportLostItemReported = "support.lost_item_reported"
)

// SupportSubjects are the support events that notify someone.
var SupportSubjects = []string{
	SubjectSupportReplyReceived,
	SubjectSupportTicketResolved,
	SubjectSupportLostItemReported,
}

// supportPayload names whom to tell. The message itself is never in it: a
// push only says there is something new on the ticket.
type supportPayload struct {
	TicketID      string `json:"ticket_id"`
	TicketNumber  string `json:"ticket_number"`
	RecipientType string `json:"recipient_type"`
	RecipientID   string `json:"recipient_id"`
}

func (h *Handler) handleSupport(ctx context.Context, subject string, envelope Envelope) error {
	var payload supportPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", subject, err)
	}

	recipientType := notification.RecipientType(payload.RecipientType)
	if !recipientType.Valid() || payload.RecipientID == "" || payload.TicketID == "" {
		h.logger.WarnContext(ctx, "support event without a usable recipient; skipping", "subject", subject)

		return nil
	}

	_, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: recipientType,
		RecipientID:   payload.RecipientID,
		EventKey:      subject,
		Variables:     map[string]string{"ticket": payload.TicketNumber},
		Data: map[string]string{
			"support_ticket_id": payload.TicketID,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send %s notification: %w", subject, err)
	}

	return nil
}
