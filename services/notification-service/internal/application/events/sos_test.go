package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

func sosEnvelope(t *testing.T, triggeredBy string) []byte {
	t.Helper()

	payload, err := json.Marshal(map[string]string{
		"trip_id":      "trip-1",
		"alert_id":     "alert-1",
		"triggered_by": triggeredBy,
		"latitude":     "36.1901",
		"longitude":    "44.0091",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	data, err := json.Marshal(events.Envelope{
		EventID:   "evt-1",
		EventType: "trip.sos_triggered",
		Payload:   json.RawMessage(payload),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	return data
}

func TestDispatchTripSOSTriggered(t *testing.T) {
	cases := []struct {
		name          string
		triggeredBy   string
		trip          events.TripInfo
		tripErr       error
		wantErr       bool
		wantSends     int
		wantType      notification.RecipientType
		wantRecipient string
	}{
		{
			name:          "rider gets the confirmation",
			triggeredBy:   "rider",
			trip:          events.TripInfo{ID: "trip-1", RiderID: "rider-1", DriverID: "driver-1"},
			wantSends:     1,
			wantType:      notification.RecipientRider,
			wantRecipient: "rider-1",
		},
		{
			name:          "driver gets the confirmation",
			triggeredBy:   "driver",
			trip:          events.TripInfo{ID: "trip-1", RiderID: "rider-1", DriverID: "driver-1"},
			wantSends:     1,
			wantType:      notification.RecipientDriver,
			wantRecipient: "driver-1",
		},
		{
			name:        "driver trigger with no driver on the trip is skipped",
			triggeredBy: "driver",
			trip:        events.TripInfo{ID: "trip-1", RiderID: "rider-1"},
			wantSends:   0,
		},
		{
			name:        "unknown triggered_by is skipped without error",
			triggeredBy: "operator",
			trip:        events.TripInfo{ID: "trip-1", RiderID: "rider-1", DriverID: "driver-1"},
			wantSends:   0,
		},
		{
			name:        "trip lookup failure returns an error so the event is redelivered",
			triggeredBy: "rider",
			tripErr:     errors.New("trip-service unavailable"),
			wantErr:     true,
			wantSends:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notifications := &fakeNotificationService{}
			trips := &fakeTripClient{trip: tc.trip, err: tc.tripErr}
			drivers := &fakeDriverClient{}

			handler := events.NewHandler(notifications, trips, drivers, testLogger())

			err := handler.Dispatch(context.Background(), "trip.sos_triggered", sosEnvelope(t, tc.triggeredBy))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Dispatch error = %v, wantErr %v", err, tc.wantErr)
			}

			if got := len(notifications.sendCalls); got != tc.wantSends {
				t.Fatalf("Send calls = %d, want %d", got, tc.wantSends)
			}

			if tc.wantSends == 0 {
				return
			}

			call := notifications.sendCalls[0]

			if call.RecipientType != tc.wantType {
				t.Errorf("RecipientType = %q, want %q", call.RecipientType, tc.wantType)
			}

			if call.RecipientID != tc.wantRecipient {
				t.Errorf("RecipientID = %q, want %q", call.RecipientID, tc.wantRecipient)
			}

			if call.EventKey != "trip.sos_confirmed" {
				t.Errorf("EventKey = %q, want trip.sos_confirmed", call.EventKey)
			}

			if call.IdempotencyKey != "evt-1" {
				t.Errorf("IdempotencyKey = %q, want evt-1", call.IdempotencyKey)
			}

			if trips.called != "trip-1" {
				t.Errorf("GetTrip called with %q, want trip-1", trips.called)
			}
		})
	}
}
