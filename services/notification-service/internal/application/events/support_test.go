package events_test

import (
	"context"
	"testing"
)

func TestSupportEventsTellTheRecipient(t *testing.T) {
	cases := map[string]string{
		"support.reply_received":     `{"ticket_id":"t-1","ticket_number":"S-000042","recipient_type":"rider","recipient_id":"rider-1"}`,
		"support.ticket_resolved":    `{"ticket_id":"t-1","ticket_number":"S-000042","recipient_type":"rider","recipient_id":"rider-1"}`,
		"support.lost_item_reported": `{"ticket_id":"t-1","ticket_number":"S-000042","recipient_type":"driver","recipient_id":"driver-1"}`,
	}

	for subject, body := range cases {
		h := newHarness()

		if err := h.handler().Dispatch(context.Background(), subject, envelopeWithPayload("evt-"+subject, body)); err != nil {
			t.Fatal(err)
		}

		if len(h.notifications.sendCalls) != 1 {
			t.Fatalf("%s: calls %+v", subject, h.notifications.sendCalls)
		}

		call := h.notifications.sendCalls[0]
		if call.EventKey != subject || call.Variables["ticket"] != "S-000042" || call.Data["support_ticket_id"] != "t-1" ||
			call.IdempotencyKey != "evt-"+subject || call.RecipientID == "" {
			t.Fatalf("%s: call %+v", subject, call)
		}
	}
}

func TestASupportEventWithoutARecipientIsSkipped(t *testing.T) {
	h := newHarness()

	payload := envelopeWithPayload("evt-s2", `{"ticket_id":"t-1","recipient_type":"staff","recipient_id":"x"}`)
	if err := h.handler().Dispatch(context.Background(), "support.reply_received", payload); err != nil || len(h.notifications.sendCalls) != 0 {
		t.Fatalf("err %v, calls %+v", err, h.notifications.sendCalls)
	}
}
