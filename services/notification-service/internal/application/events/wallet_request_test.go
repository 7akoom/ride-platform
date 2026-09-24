package events_test

import (
	"context"
	"testing"
)

func TestTheRiderAskedHearsOfTheRequest(t *testing.T) {
	h := newHarness()

	payload := envelopeWithPayload("evt-r1", `{"request_id":"request-1","code":"ABCD234XYZ","payer_rider_id":"rider-2","requester_phone":"+964770***4567","amount":"2500","currency_code":"IQD","note":"lunch"}`)
	if err := h.handler().Dispatch(context.Background(), "wallet.money_requested", payload); err != nil {
		t.Fatal(err)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientID != "rider-2" || call.EventKey != "wallet.money_requested" || call.Variables["amount"] != "2500.00" ||
		call.Variables["requester"] != "+964770***4567" || call.Data["money_request_code"] != "ABCD234XYZ" || call.IdempotencyKey != "evt-r1" {
		t.Fatalf("call %+v", call)
	}
}

func TestARequestEventWithoutAPayerIsSkipped(t *testing.T) {
	h := newHarness()

	payload := envelopeWithPayload("evt-r2", `{"request_id":"request-1","amount":"2500"}`)
	if err := h.handler().Dispatch(context.Background(), "wallet.money_requested", payload); err != nil || len(h.notifications.sendCalls) != 0 {
		t.Fatalf("err %v, calls %+v", err, h.notifications.sendCalls)
	}
}
