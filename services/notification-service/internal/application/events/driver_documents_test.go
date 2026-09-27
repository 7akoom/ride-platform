package events_test

import (
	"context"
	"testing"
)

func TestTheDriverHearsAboutTheirAccountReview(t *testing.T) {
	h := newHarness()
	handler := h.handler()

	if err := handler.Dispatch(context.Background(), "driver.approved", envelopeWithPayload("evt-d1", `{"driver_id":"driver-1"}`)); err != nil {
		t.Fatal(err)
	}

	if err := handler.Dispatch(context.Background(), "driver.rejected", envelopeWithPayload("evt-d2", `{"driver_id":"driver-1","reason":"blurry photo"}`)); err != nil {
		t.Fatal(err)
	}

	approved, rejected := h.notifications.sendCalls[0], h.notifications.sendCalls[1]

	if approved.RecipientID != "driver-1" || approved.RecipientType != "driver" || approved.EventKey != "driver.account_approved" || approved.IdempotencyKey != "evt-d1" {
		t.Errorf("approved: %+v", approved)
	}

	if rejected.EventKey != "driver.account_rejected" || rejected.Variables["reason"] != "blurry photo" {
		t.Errorf("rejected: %+v", rejected)
	}
}

func TestTheDriverHearsAboutTheirDocuments(t *testing.T) {
	names := `"document_name_en":"Driving licence (front)","document_name_ar":"إجازة السوق (الوجه)","document_name_ku":"مۆڵەتی شۆفێری (پێشەوە)"`

	cases := []struct {
		subject, payload, eventKey, variable, value string
	}{
		{"driver.document_reviewed", `{"driver_id":"driver-1","decision":"approved",` + names + `}`, "driver.document_approved", "document_ar", "إجازة السوق (الوجه)"},
		{"driver.document_reviewed", `{"driver_id":"driver-1","decision":"rejected","reason":"blurry",` + names + `}`, "driver.document_rejected", "reason", "blurry"},
		{"driver.document_reviewed", `{"driver_id":"driver-1","decision":"rejected","withdrawn":true,"reason":"forged",` + names + `}`, "driver.document_withdrawn", "reason", "forged"},
		{"driver.document_expiring", `{"driver_id":"driver-1","days_left":7,"expires_on":"2026-10-04",` + names + `}`, "driver.document_expiring", "days", "7"},
		{"driver.document_expired", `{"driver_id":"driver-1","expires_on":"2026-09-26",` + names + `}`, "driver.document_expired", "date", "2026-09-26"},
		{"driver.document_expired", `{"driver_id":"driver-1","type_code":"taxi_permit"}`, "driver.document_expired", "document_en", "taxi_permit"},
	}

	for _, c := range cases {
		h := newHarness()

		if err := h.handler().Dispatch(context.Background(), c.subject, envelopeWithPayload("evt-x", c.payload)); err != nil {
			t.Fatalf("%s: %v", c.eventKey, err)
		}

		call := h.notifications.sendCalls[0]
		if call.RecipientType != "driver" || call.RecipientID != "driver-1" || call.EventKey != c.eventKey || call.Variables[c.variable] != c.value {
			t.Errorf("%s: %+v", c.eventKey, call)
		}
	}
}

func TestADriverEventWithoutADriverIsSkipped(t *testing.T) {
	h := newHarness()

	for _, subject := range []string{"driver.approved", "driver.document_expired"} {
		if err := h.handler().Dispatch(context.Background(), subject, envelopeWithPayload("evt-y", `{}`)); err != nil {
			t.Fatal(err)
		}
	}

	if len(h.notifications.sendCalls) != 0 {
		t.Fatalf("sent %+v", h.notifications.sendCalls)
	}
}
