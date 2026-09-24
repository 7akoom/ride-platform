package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// SubjectTransferCompleted is published by wallet-service when a rider sent
// money to another.
const SubjectTransferCompleted = "wallet.transfer_completed"

type transferCompletedPayload struct {
	TransferID       string `json:"transfer_id"`
	RecipientRiderID string `json:"recipient_rider_id"`
	Amount           string `json:"amount"`
	CurrencyCode     string `json:"currency_code"`
	// SenderPhone is masked by wallet-service (+964770***4567).
	SenderPhone string `json:"sender_phone"`
}

// handleTransferCompleted tells the recipient money arrived.
func (h *Handler) handleTransferCompleted(ctx context.Context, envelope Envelope) error {
	var payload transferCompletedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode wallet.transfer_completed payload: %w", err)
	}

	if payload.RecipientRiderID == "" {
		h.logger.WarnContext(ctx, "wallet.transfer_completed without a recipient; skipping", "transfer_id", payload.TransferID)

		return nil
	}

	amount, err := decimal.NewFromString(payload.Amount)
	if err != nil {
		h.logger.WarnContext(ctx, "wallet.transfer_completed with an unreadable amount; skipping", "transfer_id", payload.TransferID)

		return nil
	}

	sender := payload.SenderPhone
	if sender == "" {
		sender = "another rider"
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   payload.RecipientRiderID,
		EventKey:      "wallet.transfer_received",
		Variables: map[string]string{
			"amount":   amount.StringFixed(2),
			"currency": payload.CurrencyCode,
			"sender":   sender,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send wallet.transfer_received notification: %w", err)
	}

	return nil
}
