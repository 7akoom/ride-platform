package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// wallet-service events about a person's own money.
const (
	SubjectTipReceived      = "wallet.tip_received"
	SubjectToppedUp         = "wallet.topped_up"
	SubjectDriverSuspended  = "wallet.driver_suspended"
	SubjectDriverReinstated = "wallet.driver_reinstated"
	SubjectPayoutPaid       = "wallet.payout_paid"
	SubjectPayoutRejected   = "wallet.payout_rejected"
	SubjectRefundIssued     = "wallet.refund_issued"
)

// WalletAccountSubjects are read by the notification-wallet-account consumer.
var WalletAccountSubjects = []string{
	SubjectTipReceived,
	SubjectToppedUp,
	SubjectDriverSuspended,
	SubjectDriverReinstated,
	SubjectPayoutPaid,
	SubjectPayoutRejected,
	SubjectRefundIssued,
}

// walletAccountPayload is the union of the fields these events carry.
type walletAccountPayload struct {
	TipID        string `json:"tip_id"`
	TopUpID      string `json:"topup_id"`
	PayoutID     string `json:"payout_id"`
	AdjustmentID string `json:"adjustment_id"`
	TripID       string `json:"trip_id"`

	RiderID   string `json:"rider_id"`
	DriverID  string `json:"driver_id"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`

	Amount       string `json:"amount"`
	AmountDue    string `json:"amount_due"`
	Balance      string `json:"balance"`
	CurrencyCode string `json:"currency_code"`
	Reason       string `json:"reason"`
}

// money formats an amount for a template; ok is false when it is unreadable.
func money(value string) (string, bool) {
	amount, err := decimal.NewFromString(value)
	if err != nil {
		return "", false
	}

	return amount.StringFixed(2), true
}

func (h *Handler) handleWalletAccount(ctx context.Context, subject string, envelope Envelope) error {
	var p walletAccountPayload

	if err := json.Unmarshal(envelope.Payload, &p); err != nil {
		return fmt.Errorf("decode %s payload: %w", subject, err)
	}

	input := notification.SendInput{
		RecipientType:  notification.RecipientDriver,
		RecipientID:    p.DriverID,
		Variables:      map[string]string{"currency": p.CurrencyCode},
		Data:           map[string]string{},
		IdempotencyKey: envelope.EventID,
	}

	amounts := map[string]string{"amount": p.Amount}

	switch subject {
	case SubjectTipReceived:
		input.EventKey = "wallet.tip_received"
		input.Data["trip_id"] = p.TripID

	case SubjectToppedUp:
		input.EventKey = "wallet.topped_up"
		input.RecipientType, input.RecipientID = notification.RecipientType(p.OwnerType), p.OwnerID
		amounts["balance"] = p.Balance
		input.Data["topup_id"] = p.TopUpID

	case SubjectDriverSuspended:
		input.EventKey = "driver.suspended"
		amounts = map[string]string{"amount_due": p.AmountDue}

	case SubjectDriverReinstated:
		input.EventKey = "driver.reinstated"
		amounts = map[string]string{}

	case SubjectPayoutPaid:
		input.EventKey = "wallet.payout_paid"
		input.Data["payout_id"] = p.PayoutID

	case SubjectPayoutRejected:
		input.EventKey = "wallet.payout_rejected"
		input.Variables["reason"] = p.Reason
		input.Data["payout_id"] = p.PayoutID

	case SubjectRefundIssued:
		input.EventKey = "wallet.refund_issued"
		input.RecipientType, input.RecipientID = notification.RecipientRider, p.RiderID
		input.Data["trip_id"] = p.TripID

	default:
		return nil
	}

	if !input.RecipientType.Valid() || input.RecipientID == "" {
		h.logger.WarnContext(ctx, "wallet event without a recipient; skipping", "subject", subject, "event_id", envelope.EventID)

		return nil
	}

	for name, value := range amounts {
		formatted, ok := money(value)
		if !ok {
			h.logger.WarnContext(ctx, "wallet event with an unreadable amount; skipping", "subject", subject, "field", name, "event_id", envelope.EventID)

			return nil
		}

		input.Variables[name] = formatted
	}

	if _, err := h.notificationService.Send(ctx, input); err != nil {
		return fmt.Errorf("send %s notification: %w", input.EventKey, err)
	}

	return nil
}
