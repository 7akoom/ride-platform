package events_test

import (
	"context"
	"testing"
)

func TestTheRecipientHearsMoneyArrived(t *testing.T) {
	h := newHarness()
	handler := h.handler()

	payload := envelopeWithPayload("evt-t1", `{"transfer_id":"transfer-1","sender_rider_id":"rider-1","recipient_rider_id":"rider-2","amount":"5000","currency_code":"IQD","sender_phone":"+964770***4567"}`)
	if err := handler.Dispatch(context.Background(), "wallet.transfer_completed", payload); err != nil {
		t.Fatal(err)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientID != "rider-2" || call.EventKey != "wallet.transfer_received" || call.Variables["amount"] != "5000.00" ||
		call.Variables["sender"] != "+964770***4567" || call.IdempotencyKey != "evt-t1" {
		t.Fatalf("call %+v", call)
	}
}

func TestATransferEventWithoutARecipientIsSkipped(t *testing.T) {
	h := newHarness()

	payload := envelopeWithPayload("evt-t2", `{"transfer_id":"transfer-1","amount":"5000"}`)
	if err := h.handler().Dispatch(context.Background(), "wallet.transfer_completed", payload); err != nil || len(h.notifications.sendCalls) != 0 {
		t.Fatalf("err %v, calls %+v", err, h.notifications.sendCalls)
	}
}
