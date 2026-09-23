package events_test

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
)

func TestTheRiderHearsTheDriverArrived(t *testing.T) {
	h := newHarness()
	h.drivers.driver = events.DriverInfo{ID: "driver-1", DisplayName: "Ali", VehicleColor: "White", VehicleModel: "Camry", PlateNumber: "ABC-123"}
	handler := h.handler()

	payload := envelopeWithPayload("evt-9", `{"trip_id":"trip-1","rider_id":"rider-1","driver_id":"driver-1"}`)
	if err := handler.Dispatch(context.Background(), "trip.driver_arrived", payload); err != nil {
		t.Fatal(err)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientID != "rider-1" || call.EventKey != "trip.driver_arrived" || call.Variables["driver_name"] != "Ali" || call.IdempotencyKey != "evt-9" {
		t.Fatalf("call %+v", call)
	}

	// The payload names the rider: the trip is not looked up.
	if h.trips.called != "" {
		t.Fatalf("GetTrip called with %q", h.trips.called)
	}
}

func TestAFeeIsNotAFinishedTrip(t *testing.T) {
	for kind, want := range map[string]string{"cancellation": "trip.cancellation_fee", "no_show": "trip.no_show_fee", "trip": "trip.completed", "": "trip.completed"} {
		h := newHarness()
		handler := h.handler()

		payload := envelopeWithPayload("evt-5", `{"trip_id":"trip-1","rider_id":"rider-1","currency_code":"IQD","total":"2000","kind":"`+kind+`"}`)
		if err := handler.Dispatch(context.Background(), "fare.calculated", payload); err != nil {
			t.Fatal(err)
		}

		if call := h.notifications.sendCalls[0]; call.EventKey != want || call.Variables["total"] != "2000.00" {
			t.Fatalf("%q: call %+v", kind, call)
		}
	}
}
