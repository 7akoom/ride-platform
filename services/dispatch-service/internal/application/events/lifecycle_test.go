package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

type lifecycleFakeTrips struct {
	driverOf map[string]string
	active   map[string]bool
	err      error

	asked []string
}

func (f *lifecycleFakeTrips) DriverOfTrip(_ context.Context, tripID string) (string, error) {
	f.asked = append(f.asked, "driver-of:"+tripID)

	return f.driverOf[tripID], f.err
}

func (f *lifecycleFakeTrips) HasActiveTrip(_ context.Context, driverID string) (bool, error) {
	f.asked = append(f.asked, "active:"+driverID)

	return f.active[driverID], f.err
}

type lifecycleFakeDrivers struct {
	availability map[string]string
	getErr       error
	markErr      error

	busy      []string
	available []string
}

func (f *lifecycleFakeDrivers) GetDriver(_ context.Context, id string) (dispatch.DriverInfo, error) {
	return dispatch.DriverInfo{ID: id, Status: "active", AvailabilityStatus: f.availability[id]}, f.getErr
}

func (f *lifecycleFakeDrivers) MarkBusy(_ context.Context, id string) error {
	f.busy = append(f.busy, id)

	return f.markErr
}

func (f *lifecycleFakeDrivers) MarkAvailable(_ context.Context, id string) error {
	f.available = append(f.available, id)

	return f.markErr
}

type lifecycleRig struct {
	trips   *lifecycleFakeTrips
	drivers *lifecycleFakeDrivers
	handler *LifecycleHandler
}

func newLifecycleRig(availability string, active bool) lifecycleRig {
	trips := &lifecycleFakeTrips{driverOf: map[string]string{"trip-1": "d1"}, active: map[string]bool{"d1": active}}
	drivers := &lifecycleFakeDrivers{availability: map[string]string{"d1": availability}}

	return lifecycleRig{
		trips:   trips,
		drivers: drivers,
		handler: NewLifecycleHandler(trips, drivers, slog.New(slog.NewTextHandler(io.Discard, nil))),
	}
}

func lifecycleEvent(t *testing.T, payload map[string]string, occurredAt time.Time) []byte {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(Envelope{EventID: "e1", AggregateType: "trip", AggregateID: payload["trip_id"], OccurredAt: occurredAt, Payload: raw})
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func (r lifecycleRig) handle(t *testing.T, subject string, payload map[string]string) error {
	t.Helper()

	return r.handler.Handle(context.Background(), subject, lifecycleEvent(t, payload, time.Now()))
}

func TestADriverWhoAcceptedATripIsMarkedBusy(t *testing.T) {
	rig := newLifecycleRig("available", true)

	if err := rig.handle(t, SubjectTripAccepted, map[string]string{"trip_id": "trip-1", "driver_id": "d1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rig.drivers.busy) != 1 || rig.drivers.busy[0] != "d1" || len(rig.drivers.available) != 0 {
		t.Errorf("expected d1 marked busy only: busy=%v available=%v", rig.drivers.busy, rig.drivers.available)
	}
}

func TestADriverWhoFinishedATripIsMarkedAvailable(t *testing.T) {
	rig := newLifecycleRig("busy", false)

	if err := rig.handle(t, SubjectTripCompleted, map[string]string{"trip_id": "trip-1", "driver_id": "d1", "rider_id": "r1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rig.drivers.available) != 1 || rig.drivers.available[0] != "d1" || len(rig.drivers.busy) != 0 {
		t.Errorf("expected d1 released only: busy=%v available=%v", rig.drivers.busy, rig.drivers.available)
	}
}

func TestACancelledTripReleasesItsDriverEvenThoughTheEventDoesNotNameThem(t *testing.T) {
	rig := newLifecycleRig("busy", false)

	if err := rig.handle(t, SubjectTripCancelled, map[string]string{"trip_id": "trip-1", "reason": "rider changed their mind"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rig.drivers.available) != 1 || rig.drivers.available[0] != "d1" {
		t.Errorf("expected d1 released: %v", rig.drivers.available)
	}

	if len(rig.trips.asked) == 0 || rig.trips.asked[0] != "driver-of:trip-1" {
		t.Errorf("the trip must be asked for its driver: %v", rig.trips.asked)
	}
}

func TestATripCancelledBeforeAnyDriverHadItChangesNobody(t *testing.T) {
	rig := newLifecycleRig("available", false)
	rig.trips.driverOf["trip-1"] = ""

	if err := rig.handle(t, SubjectTripCancelled, map[string]string{"trip_id": "trip-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rig.drivers.busy)+len(rig.drivers.available) != 0 {
		t.Errorf("nobody had the trip: busy=%v available=%v", rig.drivers.busy, rig.drivers.available)
	}
}

// The event only says "look at this driver"; the truth decides what is done.
func TestTheDriversRealStateDecidesNotTheEvent(t *testing.T) {
	cases := []struct {
		name         string
		subject      string
		availability string
		active       bool
		wantBusy     bool
		wantAvail    bool
	}{
		{"a driver already busy on a trip is left alone (accepted)", SubjectTripAccepted, "busy", true, false, false},
		{"a stale accepted event for a trip that is over does not make the driver busy", SubjectTripAccepted, "available", false, false, false},
		{"an accepted event does not bring an offline driver back", SubjectTripAccepted, "offline", true, false, false},
		{"a driver who has another trip is not released by a completed event", SubjectTripCompleted, "busy", true, false, false},
		{"a driver who went offline mid-trip stays offline when it ends", SubjectTripCompleted, "offline", false, false, false},
		{"a driver who is already available stays as they are", SubjectTripCompleted, "available", false, false, false},
		{"a cancelled event does not release a driver with another trip", SubjectTripCancelled, "busy", true, false, false},
		{"an active driver who shows as available is marked busy whatever the event", SubjectTripCompleted, "available", true, true, false},
		{"a driver with no trip who shows as busy is released whatever the event", SubjectTripAccepted, "busy", false, false, true},
	}

	for _, tc := range cases {
		rig := newLifecycleRig(tc.availability, tc.active)

		if err := rig.handle(t, tc.subject, map[string]string{"trip_id": "trip-1", "driver_id": "d1"}); err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)

			continue
		}

		if gotBusy := len(rig.drivers.busy) == 1; gotBusy != tc.wantBusy {
			t.Errorf("%s: marked busy=%v, want %v", tc.name, gotBusy, tc.wantBusy)
		}

		if gotAvail := len(rig.drivers.available) == 1; gotAvail != tc.wantAvail {
			t.Errorf("%s: marked available=%v, want %v", tc.name, gotAvail, tc.wantAvail)
		}
	}
}

func TestOtherEventsAndUnreadableOnesAreAcknowledgedAndIgnored(t *testing.T) {
	rig := newLifecycleRig("busy", false)

	if err := rig.handler.Handle(context.Background(), "trip.requested", lifecycleEvent(t, map[string]string{"trip_id": "trip-1"}, time.Now())); err != nil {
		t.Errorf("another subject: %v", err)
	}

	if err := rig.handler.Handle(context.Background(), SubjectTripCompleted, []byte("not json")); err != nil {
		t.Errorf("an unreadable event: %v", err)
	}

	if err := rig.handler.Handle(context.Background(), SubjectTripCompleted, lifecycleEvent(t, map[string]string{}, time.Now())); err != nil {
		t.Errorf("an event naming nothing: %v", err)
	}

	if len(rig.drivers.busy)+len(rig.drivers.available)+len(rig.trips.asked) != 0 {
		t.Errorf("nothing may be looked at: busy=%v available=%v asked=%v", rig.drivers.busy, rig.drivers.available, rig.trips.asked)
	}
}

func TestAServiceThatCannotBeReachedMeansTryAgainLater(t *testing.T) {
	boom := errors.New("driver-service down")

	cases := map[string]func(rig lifecycleRig){
		"the driver cannot be loaded":       func(r lifecycleRig) { r.drivers.getErr = boom },
		"the active trip cannot be checked": func(r lifecycleRig) { r.trips.err = boom },
		"the driver cannot be marked":       func(r lifecycleRig) { r.drivers.markErr = boom },
	}

	for name, break_ := range cases {
		rig := newLifecycleRig("available", true)
		break_(rig)

		err := rig.handle(t, SubjectTripAccepted, map[string]string{"trip_id": "trip-1", "driver_id": "d1"})

		var retry *retryLaterError
		if !errors.As(err, &retry) || retry.RetryDelay() != lifecycleRetryDelay || !errors.Is(err, boom) {
			t.Errorf("%s: expected a retry in %v, got %v", name, lifecycleRetryDelay, err)
		}
	}
}

func TestTheTripsDriverCannotBeFoundMeansTryAgainLater(t *testing.T) {
	rig := newLifecycleRig("busy", false)
	rig.trips.err = errors.New("trip-service down")

	err := rig.handle(t, SubjectTripCancelled, map[string]string{"trip_id": "trip-1"})

	var retry *retryLaterError
	if !errors.As(err, &retry) {
		t.Errorf("expected a retry, got %v", err)
	}
}

func TestAnEventThatKeepsFailingIsGivenUpOnEventually(t *testing.T) {
	rig := newLifecycleRig("available", true)
	rig.drivers.getErr = errors.New("driver-service down")

	old := lifecycleEvent(t, map[string]string{"trip_id": "trip-1", "driver_id": "d1"}, time.Now().Add(-lifecycleGiveUpAfter-time.Minute))

	if err := rig.handler.Handle(context.Background(), SubjectTripAccepted, old); err != nil {
		t.Errorf("an old failing event must be acknowledged, got %v", err)
	}
}

func TestReplayingHistoryLeavesEveryDriverAsTheyAre(t *testing.T) {
	// A fresh durable consumer replays the whole stream: old events for trips that
	// are long over must change nothing for a driver who is available and free.
	rig := newLifecycleRig("available", false)

	for _, subject := range LifecycleSubjects {
		if err := rig.handle(t, subject, map[string]string{"trip_id": "trip-1", "driver_id": "d1"}); err != nil {
			t.Fatalf("%s: %v", subject, err)
		}
	}

	if len(rig.drivers.busy)+len(rig.drivers.available) != 0 {
		t.Errorf("a replay changed a driver: busy=%v available=%v", rig.drivers.busy, rig.drivers.available)
	}
}

func TestTheLifecycleHandlerRequiresItsParts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for name, build := range map[string]func(){
		"no trips":   func() { NewLifecycleHandler(nil, &lifecycleFakeDrivers{}, logger) },
		"no drivers": func() { NewLifecycleHandler(&lifecycleFakeTrips{}, nil, logger) },
		"no logger":  func() { NewLifecycleHandler(&lifecycleFakeTrips{}, &lifecycleFakeDrivers{}, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected a panic", name)
				}
			}()

			build()
		}()
	}
}
