package events_test

import (
	"context"
	"testing"
)

func TestWalletAccountEventsReachTheRightPerson(t *testing.T) {
	cases := []struct {
		subject, payload                string
		recipientType, recipientID, key string
		variables                       map[string]string
		data                            map[string]string
	}{
		{
			"wallet.tip_received", `{"tip_id":"t1","trip_id":"trip-1","rider_id":"rider-1","driver_id":"driver-1","amount":"2500","currency_code":"IQD"}`,
			"driver", "driver-1", "wallet.tip_received",
			map[string]string{"amount": "2500.00", "currency": "IQD"}, map[string]string{"trip_id": "trip-1"},
		},
		{
			"wallet.topped_up", `{"topup_id":"tu1","owner_type":"rider","owner_id":"rider-1","amount":"5000","balance":"12000","currency_code":"IQD"}`,
			"rider", "rider-1", "wallet.topped_up",
			map[string]string{"amount": "5000.00", "balance": "12000.00", "currency": "IQD"}, map[string]string{"topup_id": "tu1"},
		},
		{
			"wallet.driver_suspended", `{"driver_id":"driver-1","balance":"-60000","amount_due":"60000","currency_code":"IQD"}`,
			"driver", "driver-1", "driver.suspended",
			map[string]string{"amount_due": "60000.00", "currency": "IQD"}, nil,
		},
		{
			"wallet.driver_reinstated", `{"driver_id":"driver-1","balance":"-35000","currency_code":"IQD"}`,
			"driver", "driver-1", "driver.reinstated", map[string]string{"currency": "IQD"}, nil,
		},
		{
			"wallet.payout_paid", `{"payout_id":"p1","driver_id":"driver-1","amount":"20000","currency_code":"IQD"}`,
			"driver", "driver-1", "wallet.payout_paid",
			map[string]string{"amount": "20000.00"}, map[string]string{"payout_id": "p1"},
		},
		{
			"wallet.payout_rejected", `{"payout_id":"p2","driver_id":"driver-1","amount":"10000","currency_code":"IQD","reason":"wrong number"}`,
			"driver", "driver-1", "wallet.payout_rejected",
			map[string]string{"amount": "10000.00", "reason": "wrong number"}, map[string]string{"payout_id": "p2"},
		},
		{
			"wallet.refund_issued", `{"adjustment_id":"a1","rider_id":"rider-1","trip_id":"trip-1","amount":"2000","currency_code":"IQD"}`,
			"rider", "rider-1", "wallet.refund_issued",
			map[string]string{"amount": "2000.00"}, map[string]string{"trip_id": "trip-1"},
		},
		{
			"trip.schedule_failed", `{"scheduled_trip_id":"s1","rider_id":"rider-1","reason":"no driver"}`,
			"rider", "rider-1", "trip.schedule_failed", nil, map[string]string{"scheduled_trip_id": "s1"},
		},
	}

	for _, c := range cases {
		h := newHarness()

		if err := h.handler().Dispatch(context.Background(), c.subject, envelopeWithPayload("evt-"+c.subject, c.payload)); err != nil {
			t.Fatalf("%s: %v", c.subject, err)
		}

		if len(h.notifications.sendCalls) != 1 {
			t.Fatalf("%s: %d calls", c.subject, len(h.notifications.sendCalls))
		}

		call := h.notifications.sendCalls[0]
		if string(call.RecipientType) != c.recipientType || call.RecipientID != c.recipientID || call.EventKey != c.key ||
			call.IdempotencyKey != "evt-"+c.subject {
			t.Errorf("%s: call %+v", c.subject, call)
		}

		for name, want := range c.variables {
			if call.Variables[name] != want {
				t.Errorf("%s: variable %s = %q, want %q", c.subject, name, call.Variables[name], want)
			}
		}

		for name, want := range c.data {
			if call.Data[name] != want {
				t.Errorf("%s: data %s = %q, want %q", c.subject, name, call.Data[name], want)
			}
		}

		if _, leaked := call.Variables["reason"]; leaked && c.subject == "trip.schedule_failed" {
			t.Errorf("the internal reason a booking failed reached the rider")
		}
	}
}

func TestWalletAccountEventsWithoutARecipientOrAmountAreSkipped(t *testing.T) {
	for subject, payload := range map[string]string{
		"wallet.tip_received":     `{"amount":"2500","currency_code":"IQD"}`,
		"wallet.topped_up":        `{"owner_type":"admin","owner_id":"x","amount":"5000","balance":"1"}`,
		"wallet.payout_paid":      `{"driver_id":"driver-1","amount":"lots"}`,
		"wallet.driver_suspended": `{"driver_id":"driver-1","amount_due":""}`,
		"trip.schedule_failed":    `{"scheduled_trip_id":"s1"}`,
	} {
		h := newHarness()

		if err := h.handler().Dispatch(context.Background(), subject, envelopeWithPayload("evt", payload)); err != nil || len(h.notifications.sendCalls) != 0 {
			t.Errorf("%s: err %v, calls %+v", subject, err, h.notifications.sendCalls)
		}
	}
}
