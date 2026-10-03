package events_test

import (
	"context"
	"testing"
)

func TestTheDriverHearsOfTheirIncentive(t *testing.T) {
	h := newHarness()

	payload := envelopeWithPayload("evt-i1", `{"campaign_id":"c-1","campaign_name":"Night quest","driver_id":"driver-1","amount":"15000","currency_code":"IQD","trips":26}`)
	if err := h.handler().Dispatch(context.Background(), "wallet.incentive_paid", payload); err != nil {
		t.Fatal(err)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientType != "driver" || call.RecipientID != "driver-1" || call.EventKey != "driver.incentive_earned" ||
		call.Variables["amount"] != "15000.00" || call.Variables["trips"] != "26" || call.Variables["campaign"] != "Night quest" ||
		call.Data["incentive_campaign_id"] != "c-1" || call.IdempotencyKey != "evt-i1" {
		t.Fatalf("call %+v", call)
	}
}

func TestAnIncentiveEventWithoutADriverIsSkipped(t *testing.T) {
	h := newHarness()

	payload := envelopeWithPayload("evt-i2", `{"campaign_id":"c-1","amount":"15000"}`)
	if err := h.handler().Dispatch(context.Background(), "wallet.incentive_paid", payload); err != nil || len(h.notifications.sendCalls) != 0 {
		t.Fatalf("err %v, calls %+v", err, h.notifications.sendCalls)
	}
}
