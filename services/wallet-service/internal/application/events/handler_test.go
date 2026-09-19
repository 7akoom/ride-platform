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

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/shopspring/decimal"
)

const (
	testRetryInterval = 10 * time.Second
	testGiveUpAfter   = 30 * time.Minute
)

var testNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// fakeSettler embeds the interface so only SettleTrip needs a body.
type fakeSettler struct {
	wallet.Service

	calls []wallet.SettleTripInput
	err   error
}

func (f *fakeSettler) SettleTrip(
	_ context.Context,
	input wallet.SettleTripInput,
) (wallet.Settlement, error) {
	f.calls = append(f.calls, input)

	return wallet.Settlement{}, f.err
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

func newTestHandler(settler wallet.Service, trips TripReader) *Handler {
	handler := NewHandler(
		settler,
		trips,
		wallet.PaymentCash,
		testRetryInterval,
		testGiveUpAfter,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.now = func() time.Time { return testNow }

	return handler
}

func sampleTrip() TripInfo {
	return TripInfo{ID: "trip-1", RiderID: "rider-from-trip", DriverID: "driver-1"}
}

// fareEvent builds a fare.calculated message. total is inserted as raw
// JSON so tests can use both a bare number (what pricing-service really
// publishes) and a quoted string.
func fareEvent(t *testing.T, occurredAt time.Time, payload string) []byte {
	t.Helper()

	data, err := json.Marshal(map[string]any{
		"event_id":       "evt-1",
		"event_type":     SubjectFareCalculated,
		"schema_version": 1,
		"aggregate_type": "fare",
		"aggregate_id":   "fare-1",
		"occurred_at":    occurredAt,
		"payload":        json.RawMessage(payload),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	return data
}

const barePayload = `{"trip_id":"trip-1","rider_id":"rider-1","currency_code":"IQD","total":8595.9825}`

func retryDelayOf(err error) (time.Duration, bool) {
	var delayed interface{ RetryDelay() time.Duration }

	if errors.As(err, &delayed) {
		return delayed.RetryDelay(), true
	}

	return 0, false
}

func TestHandleIgnoresOtherSubjects(t *testing.T) {
	settler := &fakeSettler{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(settler, trips)

	if err := handler.Handle(context.Background(), "trip.completed", fareEvent(t, testNow, barePayload)); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if len(settler.calls) != 0 || len(trips.calls) != 0 {
		t.Fatalf("nothing must be called for other subjects")
	}
}

func TestHandleSettlesTheTripWithTheDriverFromTheTrip(t *testing.T) {
	settler := &fakeSettler{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(settler, trips)

	err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow.Add(-time.Minute), barePayload))
	if err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(trips.calls) != 1 || trips.calls[0] != "trip-1" {
		t.Fatalf("expected one GetTrip(trip-1), got %v", trips.calls)
	}

	if len(settler.calls) != 1 {
		t.Fatalf("expected one SettleTrip call, got %d", len(settler.calls))
	}

	got := settler.calls[0]

	if got.TripID != "trip-1" || got.RiderID != "rider-1" || got.DriverID != "driver-1" {
		t.Fatalf("unexpected ids: %+v", got)
	}

	if got.PaymentMethod != wallet.PaymentCash {
		t.Fatalf("expected the default payment method cash, got %q", got.PaymentMethod)
	}

	if !got.FareAmount.Equal(decimal.RequireFromString("8595.9825")) {
		t.Fatalf("fare amount %s, want 8595.9825", got.FareAmount)
	}
}

func TestHandleAcceptsTotalAsAQuotedString(t *testing.T) {
	settler := &fakeSettler{}
	handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

	payload := `{"trip_id":"trip-1","rider_id":"rider-1","currency_code":"IQD","total":"5000"}`

	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, payload)); err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(settler.calls) != 1 || !settler.calls[0].FareAmount.Equal(decimal.RequireFromString("5000")) {
		t.Fatalf("expected one settlement of 5000, got %+v", settler.calls)
	}
}

func TestHandleFallsBackToTheTripsRider(t *testing.T) {
	settler := &fakeSettler{}
	handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

	payload := `{"trip_id":"trip-1","currency_code":"IQD","total":1000}`

	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, payload)); err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(settler.calls) != 1 || settler.calls[0].RiderID != "rider-from-trip" {
		t.Fatalf("expected the trip's rider, got %+v", settler.calls)
	}
}

func TestHandleRetriesLaterWhenTripCannotBeRead(t *testing.T) {
	settler := &fakeSettler{}
	handler := newTestHandler(settler, &fakeTrips{err: errors.New("trip-service unavailable")})

	err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload))

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}

	if len(settler.calls) != 0 {
		t.Fatalf("SettleTrip must not run when the trip can't be read")
	}
}

func TestHandleRetriesLaterOnTransientSettlementError(t *testing.T) {
	settler := &fakeSettler{err: errors.New("settle trip: connection reset")}
	handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

	err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload))

	delay, ok := retryDelayOf(err)
	if !ok || delay != testRetryInterval {
		t.Fatalf("expected delayed retry of %s, got delay=%s ok=%v (err=%v)", testRetryInterval, delay, ok, err)
	}

	if !errors.Is(err, settler.err) {
		t.Fatalf("retry error must wrap the cause")
	}
}

func TestHandleDoesNotRetryPermanentSettlementErrors(t *testing.T) {
	permanent := []error{
		wallet.ErrInvalidFareAmount,
		wallet.ErrInvalidPaymentMethod,
		wallet.ErrRiderIDRequired,
		wallet.ErrDriverIDRequired,
		wallet.ErrTripIDRequired,
	}

	for _, permanentErr := range permanent {
		settler := &fakeSettler{err: fmt.Errorf("settle: %w", permanentErr)}
		handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

		if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload)); err != nil {
			t.Fatalf("%v: expected ack (nil) for a permanent failure, got %v", permanentErr, err)
		}
	}
}

func TestHandleDropsTripWithoutADriver(t *testing.T) {
	settler := &fakeSettler{}
	handler := newTestHandler(settler, &fakeTrips{trip: TripInfo{ID: "trip-1", RiderID: "rider-1"}})

	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload)); err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(settler.calls) != 0 {
		t.Fatalf("a trip without a driver must not be settled")
	}
}

func TestHandleGivesUpAfterTheWindow(t *testing.T) {
	settler := &fakeSettler{}
	trips := &fakeTrips{trip: sampleTrip()}
	handler := newTestHandler(settler, trips)

	err := handler.Handle(
		context.Background(),
		SubjectFareCalculated,
		fareEvent(t, testNow.Add(-testGiveUpAfter-time.Second), barePayload),
	)
	if err != nil {
		t.Fatalf("expected ack (nil) once the window has passed, got %v", err)
	}

	if len(settler.calls) != 0 || len(trips.calls) != 0 {
		t.Fatalf("nothing must be called past the window")
	}
}

func TestHandleDropsUndecodableAndIncompleteMessages(t *testing.T) {
	settler := &fakeSettler{}
	handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

	messages := [][]byte{
		[]byte("not json"),
		fareEvent(t, testNow, `"just a string"`),
		fareEvent(t, testNow, `{"rider_id":"rider-1","total":1000}`),
	}

	for _, message := range messages {
		if err := handler.Handle(context.Background(), SubjectFareCalculated, message); err != nil {
			t.Fatalf("expected ack (nil) for a bad message, got %v", err)
		}
	}

	if len(settler.calls) != 0 {
		t.Fatalf("bad messages must never be settled, got %+v", settler.calls)
	}
}

func TestHandleUsesThePaymentMethodRecordedOnTheTrip(t *testing.T) {
	for raw, want := range map[string]wallet.PaymentMethod{
		"wallet":   wallet.PaymentWallet,
		" Wallet ": wallet.PaymentWallet,
		"cash":     wallet.PaymentCash,
		"card":     wallet.PaymentCard,
	} {
		settler := &fakeSettler{}
		trip := sampleTrip()
		trip.PaymentMethod = raw
		handler := newTestHandler(settler, &fakeTrips{trip: trip})

		if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload)); err != nil {
			t.Fatalf("%q: expected ack (nil), got %v", raw, err)
		}

		if len(settler.calls) != 1 || settler.calls[0].PaymentMethod != want {
			t.Fatalf("%q: expected payment method %q, got %+v", raw, want, settler.calls)
		}
	}
}

func TestHandleFallsBackToTheDefaultWhenTheTripHasNoPaymentMethod(t *testing.T) {
	settler := &fakeSettler{}
	trip := sampleTrip()
	trip.PaymentMethod = ""
	handler := newTestHandler(settler, &fakeTrips{trip: trip})

	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload)); err != nil {
		t.Fatalf("expected ack (nil), got %v", err)
	}

	if len(settler.calls) != 1 || settler.calls[0].PaymentMethod != wallet.PaymentCash {
		t.Fatalf("expected the default cash, got %+v", settler.calls)
	}
}

func TestHandlePassesAnUnknownMethodThroughSoSettleTripCanRejectIt(t *testing.T) {
	// The real SettleTrip validates the method and reports
	// ErrInvalidPaymentMethod, which the handler treats as permanent.
	settler := &fakeSettler{err: fmt.Errorf("settle: %w", wallet.ErrInvalidPaymentMethod)}
	trip := sampleTrip()
	trip.PaymentMethod = "bitcoin"
	handler := newTestHandler(settler, &fakeTrips{trip: trip})

	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow, barePayload)); err != nil {
		t.Fatalf("expected ack (nil) for a permanent failure, got %v", err)
	}

	if len(settler.calls) != 1 || settler.calls[0].PaymentMethod != wallet.PaymentMethod("bitcoin") {
		t.Fatalf("expected the raw method to reach SettleTrip, got %+v", settler.calls)
	}
}
