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

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const (
	testRetryInterval = 10 * time.Second
	testGiveUpAfter   = 30 * time.Minute
)

var testNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// fakePricer embeds the interface so only CalculateFare needs a body.
type fakePricer struct {
	pricing.Service

	calls         []pricing.CalculateFareInput
	cancellations []pricing.CancellationInput
	err           error

	released   []string
	releaseErr error
}

func (f *fakePricer) ReleaseTripCoupon(_ context.Context, tripID string) error {
	f.released = append(f.released, tripID)

	return f.releaseErr
}

func (f *fakePricer) CalculateFare(
	_ context.Context,
	input pricing.CalculateFareInput,
) (pricing.Fare, error) {
	f.calls = append(f.calls, input)

	return pricing.Fare{}, f.err
}

func (f *fakePricer) ChargeCancellation(_ context.Context, input pricing.CancellationInput) (pricing.Fare, bool, error) {
	f.cancellations = append(f.cancellations, input)

	return pricing.Fare{Kind: pricing.FareKindCancellation}, f.err == nil, f.err
}

type fakeTrips struct {
	trip  TripInfo
	err   error
	calls []string
}

func (f *fakeTrips) GetTrip(_ context.Context, tripID string) (TripInfo, error) {
	f.calls = append(f.calls, tripID)

	return f.trip, f.err
}

func newTestHandler(pricer pricing.Service, trips TripReader) *Handler {
	handler := NewHandler(
		pricer,
		trips,
		testRetryInterval,
		testGiveUpAfter,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.now = func() time.Time { return testNow }

	return handler
}

func sampleTrip() TripInfo {
	return TripInfo{
		ID:           "trip-1",
		RiderID:      "rider-1",
		PickupLat:    36.19,
		PickupLng:    44.01,
		DropoffLat:   36.2,
		DropoffLng:   44.02,
		VehicleClass: "comfort",
	}
}

func envelopeJSON(t *testing.T, aggregateID string, occurredAt time.Time, payload string) []byte {
	t.Helper()

	data, err := json.Marshal(map[string]any{
		"event_id":       "evt-1",
		"event_type":     SubjectTripCompleted,
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
	pricer := &fakePricer{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(pricer, trips)

	err := handler.Handle(context.Background(), "trip.started", envelopeJSON(t, "trip-1", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if len(pricer.calls) != 0 || len(trips.calls) != 0 {
		t.Fatalf("nothing must be called for other subjects, got pricer=%v trips=%v", pricer.calls, trips.calls)
	}
}

func TestHandleCalculatesFareFromTheTrip(t *testing.T) {
	pricer := &fakePricer{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(pricer, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripCompleted,
		envelopeJSON(t, "trip-1", testNow.Add(-time.Minute), `{}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(trips.calls) != 1 || trips.calls[0] != "trip-1" {
		t.Fatalf("expected one GetTrip(trip-1), got %v", trips.calls)
	}

	if len(pricer.calls) != 1 {
		t.Fatalf("expected one CalculateFare call, got %d", len(pricer.calls))
	}

	got := pricer.calls[0]
	want := pricing.CalculateFareInput{
		TripID:       "trip-1",
		RiderID:      "rider-1",
		PickupLat:    36.19,
		PickupLng:    44.01,
		DropoffLat:   36.2,
		DropoffLng:   44.02,
		VehicleClass: "comfort",
	}

	if got != want {
		t.Fatalf("CalculateFare input mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestHandleFallsBackToPayloadTripID(t *testing.T) {
	pricer := &fakePricer{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(pricer, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripCompleted,
		envelopeJSON(t, "", testNow, `{"trip_id":"trip-from-payload"}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(trips.calls) != 1 || trips.calls[0] != "trip-from-payload" {
		t.Fatalf("expected GetTrip(trip-from-payload), got %v", trips.calls)
	}
}

func TestHandleRetriesLaterWhenTripCannotBeRead(t *testing.T) {
	pricer := &fakePricer{}
	trips := &fakeTrips{err: errors.New("trip-service unavailable")}
	handler := newTestHandler(pricer, trips)

	err := handler.Handle(context.Background(), SubjectTripCompleted, envelopeJSON(t, "trip-1", testNow, `{}`))

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}

	if len(pricer.calls) != 0 {
		t.Fatalf("CalculateFare must not run when the trip can't be read, got %v", pricer.calls)
	}
}

func TestHandleRetriesLaterOnTransientPricingError(t *testing.T) {
	pricer := &fakePricer{err: errors.New("persist fare: connection reset")}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(pricer, trips)

	err := handler.Handle(context.Background(), SubjectTripCompleted, envelopeJSON(t, "trip-1", testNow, `{}`))

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}

	if !errors.Is(err, pricer.err) {
		t.Fatalf("retry error must wrap the cause")
	}
}

func TestHandleDoesNotRetryPermanentPricingErrors(t *testing.T) {
	permanent := []error{
		pricing.ErrPickupOutsideServiceZone,
		pricing.ErrInvalidLatitude,
		pricing.ErrInvalidLongitude,
		pricing.ErrRiderIDRequired,
		pricing.ErrInvalidVehicleClass,
	}

	for _, permanentErr := range permanent {
		pricer := &fakePricer{err: fmt.Errorf("calculate: %w", permanentErr)}
		trips := &fakeTrips{trip: sampleTrip()}
		handler := newTestHandler(pricer, trips)

		err := handler.Handle(context.Background(), SubjectTripCompleted, envelopeJSON(t, "trip-1", testNow, `{}`))
		if err != nil {
			t.Fatalf("%v: expected ack (nil) for a permanent failure, got %v", permanentErr, err)
		}
	}
}

func TestHandleGivesUpAfterTheWindow(t *testing.T) {
	pricer := &fakePricer{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(pricer, trips)

	err := handler.Handle(
		context.Background(),
		SubjectTripCompleted,
		envelopeJSON(t, "trip-1", testNow.Add(-testGiveUpAfter-time.Second), `{}`),
	)
	if err != nil {
		t.Fatalf("expected ack (nil) once the window has passed, got %v", err)
	}

	if len(pricer.calls) != 0 || len(trips.calls) != 0 {
		t.Fatalf("nothing must be called past the window, got pricer=%v trips=%v", pricer.calls, trips.calls)
	}
}

func TestHandleDropsUndecodableMessage(t *testing.T) {
	pricer := &fakePricer{}
	handler := newTestHandler(pricer, &fakeTrips{})

	if err := handler.Handle(context.Background(), SubjectTripCompleted, []byte("not json")); err != nil {
		t.Fatalf("expected ack (nil) for a poison message, got %v", err)
	}

	if len(pricer.calls) != 0 {
		t.Fatalf("poison message must not be priced, got %v", pricer.calls)
	}
}

func TestHandleDropsEventWithoutTripID(t *testing.T) {
	pricer := &fakePricer{}
	handler := newTestHandler(pricer, &fakeTrips{})

	err := handler.Handle(context.Background(), SubjectTripCompleted, envelopeJSON(t, "", testNow, `{}`))
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(pricer.calls) != 0 {
		t.Fatalf("event without a trip id must not be priced, got %v", pricer.calls)
	}
}
