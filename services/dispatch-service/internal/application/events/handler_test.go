package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

const (
	testRetryInterval = 5 * time.Second
	testSearchTimeout = 2 * time.Minute
)

var testNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

type fakeDispatcher struct {
	calls  []string
	result dispatch.Result
	err    error
}

func (f *fakeDispatcher) DispatchTrip(
	_ context.Context,
	tripID string,
	_ float64,
) (dispatch.Result, error) {
	f.calls = append(f.calls, tripID)

	return f.result, f.err
}

// fakeTrips defaults to a trip that is NOT waiting for a driver, so a test
// that never expects a cancellation cannot trigger one by accident.
type fakeTrips struct {
	status    string
	getErr    error
	cancelErr error

	getCalls    []string
	cancelCalls []cancelCall
}

type cancelCall struct {
	tripID string
	reason string
}

func (f *fakeTrips) GetTrip(_ context.Context, tripID string) (dispatch.TripInfo, error) {
	f.getCalls = append(f.getCalls, tripID)

	if f.getErr != nil {
		return dispatch.TripInfo{}, f.getErr
	}

	status := f.status
	if status == "" {
		status = "accepted"
	}

	return dispatch.TripInfo{ID: tripID, Status: status}, nil
}

func (f *fakeTrips) CancelTrip(_ context.Context, tripID string, reason string) error {
	f.cancelCalls = append(f.cancelCalls, cancelCall{tripID: tripID, reason: reason})

	return f.cancelErr
}

func newTestHandler(dispatcher dispatch.Service, trips TripCanceller) *Handler {
	handler := NewHandler(
		dispatcher,
		trips,
		testRetryInterval,
		testSearchTimeout,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.now = func() time.Time { return testNow }

	return handler
}

func envelopeJSON(t *testing.T, aggregateID string, occurredAt time.Time, payload string) []byte {
	t.Helper()

	data, err := json.Marshal(map[string]any{
		"event_id":       "evt-1",
		"event_type":     SubjectTripRequested,
		"schema_version": 1,
		"aggregate_type": "trip",
		"aggregate_id":   aggregateID,
		"occurred_at":    occurredAt,
		"payload":        json.RawMessage(payload),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	return data
}

func retryDelayOf(err error) (time.Duration, bool) {
	var delayed interface{ RetryDelay() time.Duration }

	if errors.As(err, &delayed) {
		return delayed.RetryDelay(), true
	}

	return 0, false
}

func staleAt(extra time.Duration) time.Time {
	return testNow.Add(-testSearchTimeout - extra)
}

func TestHandleIgnoresOtherSubjects(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake, &fakeTrips{})

	err := handler.Handle(context.Background(), "trip.accepted", envelopeJSON(t, "trip-1", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("dispatcher must not be called for other subjects, got %v", fake.calls)
	}
}

func TestHandleDispatchesRequestedTrip(t *testing.T) {
	fake := &fakeDispatcher{
		result: dispatch.Result{TripID: "trip-1", DriverID: "driver-1", DistanceMeters: 420},
	}
	trips := &fakeTrips{}
	handler := newTestHandler(fake, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "trip-1", testNow.Add(-3*time.Second), `{}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0] != "trip-1" {
		t.Fatalf("expected one dispatch of trip-1, got %v", fake.calls)
	}

	if len(trips.getCalls) != 0 || len(trips.cancelCalls) != 0 {
		t.Fatalf("a successful dispatch must not touch the trip, got get=%v cancel=%v", trips.getCalls, trips.cancelCalls)
	}
}

func TestHandleFallsBackToPayloadTripID(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake, &fakeTrips{})

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "", testNow, `{"trip_id":"trip-from-payload"}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0] != "trip-from-payload" {
		t.Fatalf("expected dispatch of trip-from-payload, got %v", fake.calls)
	}
}

func TestHandleAcksWhenTripNoLongerDispatchable(t *testing.T) {
	fake := &fakeDispatcher{err: fmt.Errorf("dispatch: %w", dispatch.ErrTripNotDispatchable)}
	trips := &fakeTrips{}
	handler := newTestHandler(fake, trips)

	err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "trip-1", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(trips.cancelCalls) != 0 {
		t.Fatalf("a trip that is no longer dispatchable must not be cancelled, got %v", trips.cancelCalls)
	}
}

func TestHandleRetriesLaterWhenNoDriverAvailable(t *testing.T) {
	for _, dispatchErr := range []error{dispatch.ErrNoDriversNearby, dispatch.ErrNoDriversAvailable} {
		fake := &fakeDispatcher{err: dispatchErr}
		trips := &fakeTrips{}
		handler := newTestHandler(fake, trips)

		err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "trip-1", testNow, `{}`))
		if err == nil {
			t.Fatalf("%v: expected a retry error, got nil", dispatchErr)
		}

		delay, ok := retryDelayOf(err)
		if !ok || delay != testRetryInterval {
			t.Fatalf("%v: expected delayed retry of %s, got delay=%s ok=%v", dispatchErr, testRetryInterval, delay, ok)
		}

		if !errors.Is(err, dispatchErr) {
			t.Fatalf("%v: retry error must wrap the cause", dispatchErr)
		}

		if len(trips.cancelCalls) != 0 {
			t.Fatalf("%v: no cancellation while the search window is still open", dispatchErr)
		}
	}
}

func TestHandleRetriesLaterOnTransientError(t *testing.T) {
	fake := &fakeDispatcher{err: errors.New("get trip: connection refused")}
	handler := newTestHandler(fake, &fakeTrips{})

	err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "trip-1", testNow, `{}`))

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}
}

func TestHandleCancelsATripThatIsStillWaitingOnceTheWindowPasses(t *testing.T) {
	fake := &fakeDispatcher{}
	trips := &fakeTrips{status: "requested"}
	handler := newTestHandler(fake, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "trip-1", staleAt(time.Second), `{}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("no dispatch attempt once the window has passed, got %v", fake.calls)
	}

	want := []cancelCall{{tripID: "trip-1", reason: "no drivers available"}}
	if len(trips.cancelCalls) != 1 || trips.cancelCalls[0] != want[0] {
		t.Fatalf("expected exactly %v, got %v", want, trips.cancelCalls)
	}
}

func TestHandleNeverCancelsATripThatHasADriver(t *testing.T) {
	for _, status := range []string{"accepted", "in_progress", "completed", "cancelled"} {
		trips := &fakeTrips{status: status}
		handler := newTestHandler(&fakeDispatcher{}, trips)

		err := handler.Handle(
			context.Background(),
			SubjectTripRequested,
			envelopeJSON(t, "trip-1", staleAt(time.Minute), `{}`),
		)
		if err != nil {
			t.Fatalf("%s: expected ack (nil), got %v", status, err)
		}

		if len(trips.cancelCalls) != 0 {
			t.Fatalf("%s: a trip that is not waiting for a driver must never be cancelled, got %v", status, trips.cancelCalls)
		}
	}
}

func TestHandleRetriesTheCloseOutWhenTheTripCannotBeRead(t *testing.T) {
	trips := &fakeTrips{getErr: errors.New("trip-service unavailable")}
	handler := newTestHandler(&fakeDispatcher{}, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "trip-1", staleAt(time.Second), `{}`),
	)

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}

	if len(trips.cancelCalls) != 0 {
		t.Fatalf("nothing to cancel when the trip could not be read")
	}
}

func TestHandleRetriesTheCloseOutWhenCancellationFails(t *testing.T) {
	trips := &fakeTrips{status: "requested", cancelErr: errors.New("trip-service unavailable")}
	handler := newTestHandler(&fakeDispatcher{}, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "trip-1", staleAt(time.Second), `{}`),
	)

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}

	if !errors.Is(err, trips.cancelErr) {
		t.Fatalf("retry error must wrap the cause")
	}
}

func TestHandleStopsRetryingTheCloseOutAfterTheGraceWindow(t *testing.T) {
	trips := &fakeTrips{status: "requested", cancelErr: errors.New("trip-service unavailable")}
	handler := newTestHandler(&fakeDispatcher{}, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "trip-1", staleAt(cancelRetryWindow+time.Second), `{}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil) once the grace window has passed, got %v", err)
	}
}

func TestHandleDropsUndecodableMessage(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake, &fakeTrips{})

	if err := handler.Handle(context.Background(), SubjectTripRequested, []byte("not json")); err != nil {
		t.Fatalf("expected ack (nil) for a poison message, got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("poison message must not be dispatched, got %v", fake.calls)
	}
}

func TestHandleDropsEventWithoutTripID(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake, &fakeTrips{})

	err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("event without a trip id must not be dispatched, got %v", fake.calls)
	}
}
