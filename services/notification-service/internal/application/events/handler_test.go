package events_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// --- test doubles -----------------------------------------------------

type fakeTripClient struct {
	trip   events.TripInfo
	err    error
	called string // last tripID looked up
}

func (c *fakeTripClient) GetTrip(_ context.Context, tripID string) (events.TripInfo, error) {
	c.called = tripID
	if c.err != nil {
		return events.TripInfo{}, c.err
	}
	return c.trip, nil
}

type fakeDriverClient struct {
	driver events.DriverInfo
	err    error
	called string
}

func (c *fakeDriverClient) GetDriver(_ context.Context, driverID string) (events.DriverInfo, error) {
	c.called = driverID
	if c.err != nil {
		return events.DriverInfo{}, c.err
	}
	return c.driver, nil
}

type fakeNotificationService struct {
	sendErr   error
	sendCalls []notification.SendInput
}

func (s *fakeNotificationService) Send(_ context.Context, input notification.SendInput) (notification.SendResult, error) {
	s.sendCalls = append(s.sendCalls, input)
	if s.sendErr != nil {
		return notification.SendResult{}, s.sendErr
	}
	return notification.SendResult{}, nil
}

func (s *fakeNotificationService) RegisterDevice(_ context.Context, _ notification.RegisterDeviceInput) (string, error) {
	return "", nil
}

func (s *fakeNotificationService) UnregisterDevice(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (s *fakeNotificationService) List(_ context.Context, _ notification.ListInput) (notification.ListResult, error) {
	return notification.ListResult{}, nil
}

func (s *fakeNotificationService) MarkAsRead(_ context.Context, _ notification.RecipientType, _ string, _ []string) (int, error) {
	return 0, nil
}

func (s *fakeNotificationService) UpsertTemplate(_ context.Context, _ notification.UpsertTemplateInput) (int, error) {
	return 0, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type harness struct {
	notifications *fakeNotificationService
	trips         *fakeTripClient
	drivers       *fakeDriverClient
}

func newHarness() *harness {
	return &harness{
		notifications: &fakeNotificationService{},
		trips:         &fakeTripClient{},
		drivers:       &fakeDriverClient{},
	}
}

func (h *harness) handler() *events.Handler {
	return events.NewHandler(h.notifications, h.trips, h.drivers, testLogger())
}

func envelopeWithPayload(eventID string, payload string) []byte {
	return []byte(`{"event_id":"` + eventID + `","payload":` + payload + `}`)
}

// --- NewHandler -------------------------------------------------------

func TestNewHandler_PanicsOnMissingDependencies(t *testing.T) {
	h := newHarness()

	cases := []func(){
		func() { events.NewHandler(nil, h.trips, h.drivers, testLogger()) },
		func() { events.NewHandler(h.notifications, nil, h.drivers, testLogger()) },
		func() { events.NewHandler(h.notifications, h.trips, nil, testLogger()) },
		func() { events.NewHandler(h.notifications, h.trips, h.drivers, nil) },
	}

	for i, fn := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("case %d: expected a panic", i)
				}
			}()
			fn()
		}()
	}
}

// --- Decode -------------------------------------------------------------

func TestDecode(t *testing.T) {
	envelope, err := events.Decode(envelopeWithPayload("evt-1", `{"trip_id":"trip-1"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envelope.EventID != "evt-1" {
		t.Fatalf("got %+v", envelope)
	}
}

func TestDecode_InvalidJSON(t *testing.T) {
	if _, err := events.Decode([]byte("not json")); err == nil {
		t.Fatal("expected an error decoding invalid JSON")
	}
}

// --- Dispatch: routing ------------------------------------------------

func TestHandler_Dispatch_UnknownSubjectIsAckedWithoutAction(t *testing.T) {
	h := newHarness()
	handler := h.handler()

	err := handler.Dispatch(context.Background(), "driver.registered", envelopeWithPayload("evt-1", `{}`))
	if err != nil {
		t.Fatalf("expected an unknown subject to be acked (nil error), got: %v", err)
	}

	if len(h.notifications.sendCalls) != 0 {
		t.Fatal("expected no notification to be sent for an unmapped subject")
	}
}

func TestHandler_Dispatch_InvalidEnvelopeIsAnError(t *testing.T) {
	h := newHarness()
	handler := h.handler()

	err := handler.Dispatch(context.Background(), "trip.accepted", []byte("not json"))
	if err == nil {
		t.Fatal("expected an error for an undecodable envelope")
	}
}

// --- trip.accepted ------------------------------------------------------

func TestHandler_Dispatch_TripAccepted_HappyPath(t *testing.T) {
	h := newHarness()
	h.trips.trip = events.TripInfo{ID: "trip-1", RiderID: "rider-1"}
	h.drivers.driver = events.DriverInfo{
		ID: "driver-1", DisplayName: "Ali", VehicleColor: "White",
		VehicleModel: "Camry", PlateNumber: "ABC-123",
	}
	handler := h.handler()

	payload := envelopeWithPayload("evt-1", `{"trip_id":"trip-1","driver_id":"driver-1"}`)
	if err := handler.Dispatch(context.Background(), "trip.accepted", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.trips.called != "trip-1" {
		t.Fatalf("expected GetTrip to be called with trip-1, got %q", h.trips.called)
	}
	if h.drivers.called != "driver-1" {
		t.Fatalf("expected GetDriver to be called with driver-1, got %q", h.drivers.called)
	}

	if len(h.notifications.sendCalls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(h.notifications.sendCalls))
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientType != notification.RecipientRider || call.RecipientID != "rider-1" {
		t.Fatalf("unexpected recipient: %+v", call)
	}
	if call.EventKey != "trip.driver_assigned" {
		t.Fatalf("got event key %q", call.EventKey)
	}
	if call.IdempotencyKey != "evt-1" {
		t.Fatalf("expected the idempotency key to be the outbox event id, got %q", call.IdempotencyKey)
	}
	if call.Variables["driver_name"] != "Ali" || call.Variables["plate_number"] != "ABC-123" {
		t.Fatalf("unexpected variables: %+v", call.Variables)
	}
}

func TestHandler_Dispatch_TripAccepted_WrapsGetTripError(t *testing.T) {
	h := newHarness()
	h.trips.err = errors.New("trip-service unreachable")
	handler := h.handler()

	payload := envelopeWithPayload("evt-1", `{"trip_id":"trip-1","driver_id":"driver-1"}`)
	err := handler.Dispatch(context.Background(), "trip.accepted", payload)
	if !errors.Is(err, h.trips.err) {
		t.Fatalf("got %v, want wrapped %v", err, h.trips.err)
	}

	if len(h.notifications.sendCalls) != 0 {
		t.Fatal("expected no notification to be sent when the trip lookup fails")
	}
}

func TestHandler_Dispatch_TripAccepted_WrapsGetDriverError(t *testing.T) {
	h := newHarness()
	h.drivers.err = errors.New("driver-service unreachable")
	handler := h.handler()

	payload := envelopeWithPayload("evt-1", `{"trip_id":"trip-1","driver_id":"driver-1"}`)
	err := handler.Dispatch(context.Background(), "trip.accepted", payload)
	if !errors.Is(err, h.drivers.err) {
		t.Fatalf("got %v, want wrapped %v", err, h.drivers.err)
	}
}

func TestHandler_Dispatch_TripAccepted_WrapsSendError(t *testing.T) {
	h := newHarness()
	h.notifications.sendErr = errors.New("template not found")
	handler := h.handler()

	payload := envelopeWithPayload("evt-1", `{"trip_id":"trip-1","driver_id":"driver-1"}`)
	err := handler.Dispatch(context.Background(), "trip.accepted", payload)
	if !errors.Is(err, h.notifications.sendErr) {
		t.Fatalf("got %v, want wrapped %v", err, h.notifications.sendErr)
	}
}

// --- trip.started ---------------------------------------------------------

func TestHandler_Dispatch_TripStarted_RendersDropoffCoordinates(t *testing.T) {
	h := newHarness()
	h.trips.trip = events.TripInfo{ID: "trip-1", RiderID: "rider-1", DropoffLatitude: 36.19, DropoffLongitude: 44.01}
	handler := h.handler()

	payload := envelopeWithPayload("evt-2", `{"trip_id":"trip-1"}`)
	if err := handler.Dispatch(context.Background(), "trip.started", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := h.notifications.sendCalls[0]
	if call.EventKey != "trip.started" {
		t.Fatalf("got event key %q", call.EventKey)
	}
	if call.Variables["dropoff"] != "36.19000, 44.01000" {
		t.Fatalf("got dropoff %q", call.Variables["dropoff"])
	}
}

// --- trip.cancelled ---------------------------------------------------------

func TestHandler_Dispatch_TripCancelled_PassesReasonThrough(t *testing.T) {
	h := newHarness()
	h.trips.trip = events.TripInfo{ID: "trip-1", RiderID: "rider-1"}
	handler := h.handler()

	payload := envelopeWithPayload("evt-3", `{"trip_id":"trip-1","reason":"rider changed mind"}`)
	if err := handler.Dispatch(context.Background(), "trip.cancelled", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := h.notifications.sendCalls[0]
	if call.EventKey != "trip.cancelled" || call.Variables["reason"] != "rider changed mind" {
		t.Fatalf("unexpected call: %+v", call)
	}
}

// --- fare.calculated ---------------------------------------------------------

func TestHandler_Dispatch_FareCalculated_FormatsTotalToTwoDecimals(t *testing.T) {
	h := newHarness()
	handler := h.handler()

	payload := envelopeWithPayload("evt-4", `{"trip_id":"trip-1","rider_id":"rider-1","currency_code":"IQD","total":4250.5}`)
	if err := handler.Dispatch(context.Background(), "fare.calculated", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientID != "rider-1" || call.EventKey != "trip.completed" {
		t.Fatalf("unexpected call: %+v", call)
	}
	if call.Variables["total"] != "4250.50" || call.Variables["currency"] != "IQD" {
		t.Fatalf("unexpected variables: %+v", call.Variables)
	}

	// fare.calculated never looks up the trip — everything it needs is
	// already on the event payload.
	if h.trips.called != "" {
		t.Fatalf("expected GetTrip not to be called, got %q", h.trips.called)
	}
}
