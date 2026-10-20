package events_test

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
)

func TestTheRiderHearsTheDriverSetOffAfterTheirPreviousTrip(t *testing.T) {
	h := newHarness()
	h.drivers.driver = events.DriverInfo{ID: "driver-1", DisplayName: "Ali", VehicleColor: "White", VehicleModel: "Camry", PlateNumber: "ABC-123"}
	handler := h.handler()

	payload := envelopeWithPayload("evt-11", `{"trip_id":"trip-2","rider_id":"rider-2","driver_id":"driver-1","after_trip_id":"trip-1"}`)
	if err := handler.Dispatch(context.Background(), events.SubjectDriverHeading, payload); err != nil {
		t.Fatal(err)
	}

	if len(h.notifications.sendCalls) != 1 {
		t.Fatalf("calls %+v", h.notifications.sendCalls)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientID != "rider-2" || call.EventKey != "trip.driver_heading" || call.Variables["driver_name"] != "Ali" || call.IdempotencyKey != "evt-11" {
		t.Fatalf("call %+v", call)
	}
}

func TestAHeadingEventWithoutARiderIsSkipped(t *testing.T) {
	h := newHarness()
	handler := h.handler()

	if err := handler.Dispatch(context.Background(), events.SubjectDriverHeading, envelopeWithPayload("evt-12", `{"trip_id":"trip-2"}`)); err != nil {
		t.Fatal(err)
	}

	if len(h.notifications.sendCalls) != 0 {
		t.Fatalf("calls %+v", h.notifications.sendCalls)
	}
}

func TestANextTripIsAssignedWithItsOwnMessage(t *testing.T) {
	h := newHarness()
	h.trips.trip = events.TripInfo{ID: "trip-2", RiderID: "rider-2", WaitingForPreviousTrip: true}
	h.drivers.driver = events.DriverInfo{ID: "driver-1", DisplayName: "Ali"}
	handler := h.handler()

	if err := handler.Dispatch(context.Background(), "trip.accepted", envelopeWithPayload("evt-13", `{"trip_id":"trip-2","driver_id":"driver-1"}`)); err != nil {
		t.Fatal(err)
	}

	if call := h.notifications.sendCalls[0]; call.EventKey != "trip.driver_assigned_next" || call.RecipientID != "rider-2" {
		t.Fatalf("call %+v", call)
	}
}
