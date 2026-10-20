package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func newRequestedTrip(t *testing.T, repo *TripRepository) trip.Trip {
	t.Helper()

	created, err := repo.Create(context.Background(), trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.19, Longitude: 44.01},
		Dropoff: trip.Coordinates{Latitude: 36.21, Longitude: 44.03},
	})
	if err != nil {
		t.Fatal(err)
	}

	return created
}

// onTrip gives the driver a trip of their own, accepted through an offer and
// started (the rider in the car).
func onTrip(t *testing.T, repo *TripRepository, driver string) trip.Trip {
	t.Helper()
	ctx := context.Background()

	current := newRequestedTrip(t, repo)

	if _, err := repo.CreateOffer(ctx, current.ID, driver, 15*time.Second); err != nil {
		t.Fatal(err)
	}

	accepted, err := repo.AcceptOffer(ctx, current.ID, driver)
	if err != nil {
		t.Fatal(err)
	}

	if accepted.HeadingAt == nil || accepted.WaitingForPreviousTrip() {
		t.Fatalf("an ordinary trip sets off when accepted: %+v", accepted)
	}

	started, err := repo.Start(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}

	return started
}

func TestANextTripWaitsForTheCurrentOneAndThenStarts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	driver := uuid.NewString()

	current := onTrip(t, repo, driver)
	next := newRequestedTrip(t, repo)

	// An ordinary offer to a driver on a trip is refused, as before.
	if _, err := repo.CreateOffer(ctx, next.ID, driver, 15*time.Second); !errors.Is(err, trip.ErrDriverHasActiveTrip) {
		t.Fatalf("plain offer to a busy driver: %v", err)
	}

	offer, err := repo.CreateChainedOffer(ctx, next.ID, driver, 15*time.Second, trip.OfferChain{AfterTripID: current.ID, PickupETA: 7 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := repo.FindPendingOffer(ctx, driver)
	if err != nil || pending.TripID != next.ID || pending.AfterTripID != current.ID || pending.PickupETA != 7*time.Minute {
		t.Fatalf("pending offer %+v, %v", pending, err)
	}

	accepted, err := repo.AcceptOffer(ctx, next.ID, driver)
	if err != nil {
		t.Fatal(err)
	}

	if !accepted.WaitingForPreviousTrip() || accepted.AfterTripID != current.ID || accepted.ExpectedPickupAt == nil {
		t.Fatalf("the next trip waits for the current one: %+v", accepted)
	}

	if got := accepted.ExpectedPickupAt.Sub(offer.OfferedAt); got != 7*time.Minute {
		t.Errorf("expected at offered + 7 min, got +%v", got)
	}

	// The driver's current trip is still the first; the next one is apart.
	active, err := repo.FindActiveByDriverID(ctx, driver)
	if err != nil || active.ID != current.ID {
		t.Fatalf("current trip %s, %v; want %s", active.ID, err, current.ID)
	}

	found, err := repo.FindNextByDriverID(ctx, driver)
	if err != nil || found.ID != next.ID {
		t.Fatalf("next trip %s, %v; want %s", found.ID, err, next.ID)
	}

	// Not at that pickup yet, and it cannot start.
	if _, err := repo.MarkArrived(ctx, next.ID); !errors.Is(err, trip.ErrPreviousTripNotFinished) {
		t.Fatalf("arriving at the next pickup early: %v", err)
	}

	if _, err := repo.Start(ctx, next.ID); !errors.Is(err, trip.ErrPreviousTripNotFinished) {
		t.Fatalf("starting the next trip early: %v", err)
	}

	// A third trip cannot be put after it.
	third := newRequestedTrip(t, repo)
	if _, err := repo.CreateChainedOffer(ctx, third.ID, driver, 15*time.Second, trip.OfferChain{AfterTripID: current.ID}); !errors.Is(err, trip.ErrNotOfferableAfterTrip) {
		t.Fatalf("a second next trip: %v", err)
	}

	if _, err := repo.Complete(ctx, current.ID); err != nil {
		t.Fatal(err)
	}

	active, err = repo.FindActiveByDriverID(ctx, driver)
	if err != nil || active.ID != next.ID || active.HeadingAt == nil || active.WaitingForPreviousTrip() {
		t.Fatalf("once the first ends the next is current: %+v, %v", active, err)
	}

	if _, err := repo.FindNextByDriverID(ctx, driver); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("no next trip any more: %v", err)
	}

	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE event_type = 'trip.driver_heading' AND aggregate_id = $1`, next.ID).Scan(&raw); err != nil {
		t.Fatalf("one trip.driver_heading: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil || payload["rider_id"] != next.RiderID || payload["driver_id"] != driver || payload["after_trip_id"] != current.ID {
		t.Fatalf("payload %s %v", raw, err)
	}

	if _, err := repo.MarkArrived(ctx, next.ID); err != nil {
		t.Fatalf("arriving once it is current: %v", err)
	}
}

func TestCancellingTheCurrentTripSendsTheDriverToTheNextOne(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	driver := uuid.NewString()

	current := onTrip(t, repo, driver)
	next := newRequestedTrip(t, repo)

	if _, err := repo.CreateChainedOffer(ctx, next.ID, driver, 15*time.Second, trip.OfferChain{AfterTripID: current.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.AcceptOffer(ctx, next.ID, driver); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Cancel(ctx, trip.CancelRecord{TripID: current.ID, By: trip.CancelledByRider}); err != nil {
		t.Fatal(err)
	}

	active, err := repo.FindActiveByDriverID(ctx, driver)
	if err != nil || active.ID != next.ID || active.WaitingForPreviousTrip() {
		t.Fatalf("the next trip is current: %+v, %v", active, err)
	}
}

func TestACancelledNextTripStaysWaitingAndLeavesTheCurrentOne(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	driver := uuid.NewString()

	current := onTrip(t, repo, driver)
	next := newRequestedTrip(t, repo)

	if _, err := repo.CreateChainedOffer(ctx, next.ID, driver, 15*time.Second, trip.OfferChain{AfterTripID: current.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.AcceptOffer(ctx, next.ID, driver); err != nil {
		t.Fatal(err)
	}

	cancelled, err := repo.Cancel(ctx, trip.CancelRecord{TripID: next.ID, By: trip.CancelledByRider})
	if err != nil {
		t.Fatal(err)
	}

	// Pricing reads this to know the driver never set off: cancelling cost nothing.
	if !cancelled.WaitingForPreviousTrip() {
		t.Fatalf("a next trip cancelled before the driver set off: %+v", cancelled)
	}

	active, err := repo.FindActiveByDriverID(ctx, driver)
	if err != nil || active.ID != current.ID {
		t.Fatalf("the current trip goes on: %+v, %v", active, err)
	}

	if _, err := repo.Complete(ctx, current.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FindActiveByDriverID(ctx, driver); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("nothing left: %v", err)
	}
}

func TestAnOfferAfterATripThatEndedMeanwhileIsAnOrdinaryTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	driver := uuid.NewString()

	current := onTrip(t, repo, driver)
	next := newRequestedTrip(t, repo)

	if _, err := repo.CreateChainedOffer(ctx, next.ID, driver, 15*time.Second, trip.OfferChain{AfterTripID: current.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Complete(ctx, current.ID); err != nil {
		t.Fatal(err)
	}

	accepted, err := repo.AcceptOffer(ctx, next.ID, driver)
	if err != nil {
		t.Fatal(err)
	}

	if accepted.WaitingForPreviousTrip() || accepted.HeadingAt == nil {
		t.Fatalf("the driver sets off at once: %+v", accepted)
	}
}

func TestAnOfferAfterATripIsOnlyForItsDriverWithTheRiderInTheCar(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	driver, other := uuid.NewString(), uuid.NewString()

	// Accepted, not started: the driver is still on their way to that rider.
	current := newRequestedTrip(t, repo)
	if _, err := repo.CreateOffer(ctx, current.ID, driver, 15*time.Second); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.AcceptOffer(ctx, current.ID, driver); err != nil {
		t.Fatal(err)
	}

	next := newRequestedTrip(t, repo)

	for name, who := range map[string]string{"before the rider is in the car": driver, "to another driver": other} {
		if _, err := repo.CreateChainedOffer(ctx, next.ID, who, 15*time.Second, trip.OfferChain{AfterTripID: current.ID}); !errors.Is(err, trip.ErrNotOfferableAfterTrip) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
