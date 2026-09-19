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

func newTestHandler(dispatcher dispatch.Service) *Handler {
	handler := NewHandler(
		dispatcher,
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

func TestHandleIgnoresOtherSubjects(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake)

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
	handler := newTestHandler(fake)

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
}

func TestHandleFallsBackToPayloadTripID(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake)

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
	handler := newTestHandler(fake)

	err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "trip-1", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}
}

func TestHandleRetriesLaterWhenNoDriverAvailable(t *testing.T) {
	for _, dispatchErr := range []error{dispatch.ErrNoDriversNearby, dispatch.ErrNoDriversAvailable} {
		fake := &fakeDispatcher{err: dispatchErr}
		handler := newTestHandler(fake)

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
	}
}

func TestHandleRetriesLaterOnTransientError(t *testing.T) {
	fake := &fakeDispatcher{err: errors.New("get trip: connection refused")}
	handler := newTestHandler(fake)

	err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "trip-1", testNow, `{}`))

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}
}

func TestHandleSkipsStaleEventWithoutDispatching(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake)

	err := handler.Handle(
		context.Background(),
		SubjectTripRequested,
		envelopeJSON(t, "trip-1", testNow.Add(-testSearchTimeout-time.Second), `{}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil) for a stale event, got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("stale event must not be dispatched, got %v", fake.calls)
	}
}

func TestHandleDropsUndecodableMessage(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake)

	if err := handler.Handle(context.Background(), SubjectTripRequested, []byte("not json")); err != nil {
		t.Fatalf("expected ack (nil) for a poison message, got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("poison message must not be dispatched, got %v", fake.calls)
	}
}

func TestHandleDropsEventWithoutTripID(t *testing.T) {
	fake := &fakeDispatcher{}
	handler := newTestHandler(fake)

	err := handler.Handle(context.Background(), SubjectTripRequested, envelopeJSON(t, "", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("event without a trip id must not be dispatched, got %v", fake.calls)
	}
}
