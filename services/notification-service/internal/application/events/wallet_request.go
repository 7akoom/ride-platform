package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// SubjectMoneyRequested is published by wallet-service when a rider asked
// another (by phone) for money. An open request (a link or a QR code) has
// no one to tell and publishes nothing.
const SubjectMoneyRequested = "wallet.money_requested"

type moneyRequestedPayload struct {
	RequestID    string `json:"request_id"`
	Code         string `json:"code"`
	PayerRiderID string `json:"payer_rider_id"`
	// RequesterPhone is masked by wallet-service (+964770***4567).
	RequesterPhone string `json:"requester_phone"`
	Amount         string `json:"amount"`
	CurrencyCode   string `json:"currency_code"`
	Note           string `json:"note"`
}

// handleMoneyRequested tells the rider asked. The notification's data
// carries the request's code, which the app opens it with.
func (h *Handler) handleMoneyRequested(ctx context.Context, envelope Envelope) error {
	var payload moneyRequestedPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode wallet.money_requested payload: %w", err)
	}

	if payload.PayerRiderID == "" {
		h.logger.WarnContext(ctx, "wallet.money_requested without a payer; skipping", "request_id", payload.RequestID)

		return nil
	}

	amount, err := decimal.NewFromString(payload.Amount)
	if err != nil {
		h.logger.WarnContext(ctx, "wallet.money_requested with an unreadable amount; skipping", "request_id", payload.RequestID)

		return nil
	}

	requester := payload.RequesterPhone
	if requester == "" {
		requester = "another rider"
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientRider,
		RecipientID:   payload.PayerRiderID,
		EventKey:      "wallet.money_requested",
		Variables: map[string]string{
			"amount":    amount.StringFixed(2),
			"currency":  payload.CurrencyCode,
			"requester": requester,
		},
		Data: map[string]string{
			"money_request_code": payload.Code,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send wallet.money_requested notification: %w", err)
	}

	return nil
}
