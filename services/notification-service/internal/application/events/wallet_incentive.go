package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// SubjectIncentivePaid is published by wallet-service when an incentive
// campaign's bonus was paid into a driver's wallet.
const SubjectIncentivePaid = "wallet.incentive_paid"

type incentivePaidPayload struct {
	CampaignID   string `json:"campaign_id"`
	CampaignName string `json:"campaign_name"`
	DriverID     string `json:"driver_id"`
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currency_code"`
	Trips        int    `json:"trips"`
}

func (h *Handler) handleIncentivePaid(ctx context.Context, envelope Envelope) error {
	var payload incentivePaidPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode wallet.incentive_paid payload: %w", err)
	}

	if payload.DriverID == "" {
		h.logger.WarnContext(ctx, "wallet.incentive_paid without a driver; skipping", "campaign_id", payload.CampaignID)

		return nil
	}

	amount, err := decimal.NewFromString(payload.Amount)
	if err != nil {
		h.logger.WarnContext(ctx, "wallet.incentive_paid with an unreadable amount; skipping", "campaign_id", payload.CampaignID)

		return nil
	}

	_, err = h.notificationService.Send(ctx, notification.SendInput{
		RecipientType: notification.RecipientDriver,
		RecipientID:   payload.DriverID,
		EventKey:      "driver.incentive_earned",
		Variables: map[string]string{
			"amount":   amount.StringFixed(2),
			"currency": payload.CurrencyCode,
			"trips":    strconv.Itoa(payload.Trips),
			"campaign": payload.CampaignName,
		},
		Data: map[string]string{
			"incentive_campaign_id": payload.CampaignID,
		},
		IdempotencyKey: envelope.EventID,
	})
	if err != nil {
		return fmt.Errorf("send driver.incentive_earned notification: %w", err)
	}

	return nil
}
