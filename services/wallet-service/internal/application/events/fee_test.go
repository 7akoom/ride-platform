package events

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestAFeeEventSettlesAFee(t *testing.T) {
	settler := &fakeSettler{}
	handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

	payload := `{"trip_id":"trip-1","rider_id":"rider-1","currency_code":"IQD","total":"2000","kind":"no_show"}`
	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow.Add(-time.Minute), payload)); err != nil {
		t.Fatal(err)
	}

	if got := settler.calls[0]; got.Kind != wallet.SettlementNoShow || got.DriverID != "driver-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestAnInvalidKindIsDropped(t *testing.T) {
	settler := &fakeSettler{err: wallet.ErrInvalidSettlementKind}
	handler := newTestHandler(settler, &fakeTrips{trip: sampleTrip()})

	payload := `{"trip_id":"trip-1","rider_id":"rider-1","currency_code":"IQD","total":"2000","kind":"refund"}`
	if err := handler.Handle(context.Background(), SubjectFareCalculated, fareEvent(t, testNow.Add(-time.Minute), payload)); err != nil {
		t.Fatalf("expected an ack, got %v", err)
	}
}
