package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func cancelledTrip() TripInfo {
	trip := sampleTrip()
	accepted, arrived, cancelled := testNow.Add(-10*time.Minute), testNow.Add(-8*time.Minute), testNow.Add(-time.Minute)
	trip.Status = "cancelled"
	trip.DriverID = "driver-1"
	trip.CancelledBy = "driver"
	trip.RiderNoShow = true
	trip.AcceptedAt, trip.ArrivedAt, trip.CancelledAt = &accepted, &arrived, &cancelled

	return trip
}

func TestACancelledTripIsCheckedForAFee(t *testing.T) {
	pricer := &fakePricer{}
	handler := newTestHandler(pricer, &fakeTrips{trip: cancelledTrip()})

	if err := handler.Handle(context.Background(), SubjectTripCancelled, envelopeJSON(t, "trip-1", testNow, `{}`)); err != nil {
		t.Fatal(err)
	}

	if len(pricer.calls) != 0 || len(pricer.cancellations) != 1 {
		t.Fatalf("calls %v, cancellations %v", pricer.calls, pricer.cancellations)
	}

	got := pricer.cancellations[0]
	if got.TripID != "trip-1" || got.DriverID != "driver-1" || got.CancelledBy != "driver" || !got.RiderNoShow ||
		got.ArrivedAt == nil || got.VehicleClass != "comfort" || got.PickupLat != 36.19 {
		t.Fatalf("input %+v", got)
	}
}

func TestCancellationFeeFailures(t *testing.T) {
	// A trip that is not cancelled (an event out of order) is left alone.
	pricer := &fakePricer{}
	trip := cancelledTrip()
	trip.Status = "completed"

	if err := newTestHandler(pricer, &fakeTrips{trip: trip}).Handle(context.Background(), SubjectTripCancelled, envelopeJSON(t, "trip-1", testNow, `{}`)); err != nil || len(pricer.cancellations) != 0 {
		t.Fatalf("err %v, cancellations %v", err, pricer.cancellations)
	}

	// A passing failure is retried.
	pricer = &fakePricer{err: errors.New("database down")}
	err := newTestHandler(pricer, &fakeTrips{trip: cancelledTrip()}).Handle(context.Background(), SubjectTripCancelled, envelopeJSON(t, "trip-1", testNow, `{}`))
	if _, retried := retryDelayOf(err); !retried {
		t.Fatalf("expected a retry, got %v", err)
	}

	// One that cannot pass is dropped.
	pricer = &fakePricer{err: pricing.ErrQuoteNotForTrip}
	if err := newTestHandler(pricer, &fakeTrips{trip: cancelledTrip()}).Handle(context.Background(), SubjectTripCancelled, envelopeJSON(t, "trip-1", testNow, `{}`)); err != nil {
		t.Fatalf("expected an ack, got %v", err)
	}
}

func TestACompletedTripCarriesItsWaiting(t *testing.T) {
	pricer := &fakePricer{}
	trip := cancelledTrip()
	trip.Status = "completed"
	started := testNow.Add(-5 * time.Minute)
	trip.StartedAt = &started

	if err := newTestHandler(pricer, &fakeTrips{trip: trip}).Handle(context.Background(), SubjectTripCompleted, envelopeJSON(t, "trip-1", testNow, `{}`)); err != nil {
		t.Fatal(err)
	}

	if got := pricer.calls[0]; got.ArrivedAt == nil || got.StartedAt == nil || !got.StartedAt.Equal(started) {
		t.Fatalf("input %+v", got)
	}
}

func TestACancelledTripFreesItsCoupon(t *testing.T) {
	pricer := &fakePricer{}
	if err := newTestHandler(pricer, &fakeTrips{trip: cancelledTrip()}).Handle(context.Background(), SubjectTripCancelled, envelopeJSON(t, "trip-1", testNow, `{}`)); err != nil {
		t.Fatal(err)
	}

	if len(pricer.released) != 1 || pricer.released[0] != "trip-1" {
		t.Fatalf("released %v", pricer.released)
	}

	// Freeing it failed: the event comes back, and no fee is charged yet.
	pricer = &fakePricer{releaseErr: errors.New("database down")}
	err := newTestHandler(pricer, &fakeTrips{trip: cancelledTrip()}).Handle(context.Background(), SubjectTripCancelled, envelopeJSON(t, "trip-1", testNow, `{}`))
	if _, retried := retryDelayOf(err); !retried || len(pricer.cancellations) != 0 {
		t.Fatalf("expected a retry before any fee, got %v, %v", err, pricer.cancellations)
	}

	// A completed trip keeps it.
	pricer = &fakePricer{}
	trip := cancelledTrip()
	trip.Status = "completed"
	if err := newTestHandler(pricer, &fakeTrips{trip: trip}).Handle(context.Background(), SubjectTripCompleted, envelopeJSON(t, "trip-1", testNow, `{}`)); err != nil || len(pricer.released) != 0 {
		t.Fatalf("err %v, released %v", err, pricer.released)
	}
}
