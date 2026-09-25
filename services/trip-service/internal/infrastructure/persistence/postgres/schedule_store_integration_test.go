package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/schedule"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func booking(rider, key string, at time.Time) schedule.Ride {
	return schedule.Ride{
		ID: uuid.NewString(), RiderID: rider, IdempotencyKey: key, ScheduledAt: at, TimeZone: "Asia/Baghdad",
		Pickup:        trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff:       trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		PickupAddress: "Home", DropoffAddress: "Airport", VehicleClass: "economy", PaymentMethod: "cash",
		PassengerName: "Ahmed", PassengerPhone: "+9647500000001",
		NextAttemptAt: at.Add(-10 * time.Minute),
	}
}

func TestBookingsAreCountedKeptAndCancelled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewScheduleStore(pool)
	rider := uuid.NewString()
	soon := time.Now().Add(2 * time.Hour).Truncate(time.Microsecond)

	first, existed, err := store.Create(ctx, booking(rider, "k1", soon), 2)
	if err != nil || existed || first.Status != schedule.Scheduled || first.PassengerName != "Ahmed" || first.TimeZone != "Asia/Baghdad" {
		t.Fatalf("first %+v %v %v", first, existed, err)
	}

	// The same key: the same booking, nothing new.
	again, existed, err := store.Create(ctx, booking(rider, "k1", soon.Add(time.Hour)), 2)
	if err != nil || !existed || again.ID != first.ID {
		t.Fatalf("again %+v %v %v", again, existed, err)
	}

	if _, _, err := store.Create(ctx, booking(rider, "k2", soon.Add(time.Hour)), 2); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Create(ctx, booking(rider, "k3", soon.Add(2*time.Hour)), 2); !errors.Is(err, schedule.ErrTooManyUpcoming) {
		t.Fatalf("a third upcoming: %v", err)
	}

	upcoming, err := store.ListForRider(ctx, rider, false, 50)
	if err != nil || len(upcoming) != 2 || upcoming[0].ID != first.ID {
		t.Fatalf("upcoming, soonest first %+v %v", upcoming, err)
	}

	if _, err := store.Cancel(ctx, first.ID, uuid.NewString(), time.Now()); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("another rider's: %v", err)
	}

	cancelled, err := store.Cancel(ctx, first.ID, rider, time.Now())
	if err != nil || cancelled.Status != schedule.Cancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancel %+v %v", cancelled, err)
	}

	if _, err := store.Cancel(ctx, first.ID, rider, time.Now()); !errors.Is(err, schedule.ErrNotScheduled) {
		t.Fatalf("cancelling twice: %v", err)
	}

	if _, err := store.Cancel(ctx, "not-an-id", rider, time.Now()); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("a bad id: %v", err)
	}

	// A cancelled booking frees a place.
	if _, _, err := store.Create(ctx, booking(rider, "k3", soon.Add(2*time.Hour)), 2); err != nil {
		t.Fatalf("after a cancel: %v", err)
	}

	all, _ := store.ListForRider(ctx, rider, true, 50)
	if len(all) != 3 {
		t.Fatalf("past and upcoming %d", len(all))
	}
}

func TestDueBookingsAreClaimedOnceAndClosedOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewScheduleStore(pool)
	now := time.Now()

	due := booking(uuid.NewString(), "due", now.Add(5*time.Minute))
	later := booking(uuid.NewString(), "later", now.Add(3*time.Hour))

	for _, b := range []schedule.Ride{due, later} {
		if _, _, err := store.Create(ctx, b, 3); err != nil {
			t.Fatal(err)
		}
	}

	// Two schedulers at once: the due booking goes to one of them.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed []schedule.Ride
	)

	for i := 0; i < 4; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			got, err := store.ClaimDue(ctx, now, time.Minute, 10)
			if err != nil {
				t.Error(err)
			}

			mu.Lock()
			claimed = append(claimed, got...)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(claimed) != 1 || claimed[0].ID != due.ID || claimed[0].Attempts != 1 {
		t.Fatalf("claimed %+v", claimed)
	}

	// Leased: not due again until the lease ends.
	if got, _ := store.ClaimDue(ctx, now.Add(30*time.Second), time.Minute, 10); len(got) != 0 {
		t.Fatalf("claimed while leased %+v", got)
	}

	if err := store.Retry(ctx, due.ID, "rider already has an active trip", now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}

	tripID := uuid.NewString()
	if err := store.MarkDispatched(ctx, due.ID, tripID, now); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkDispatched(ctx, due.ID, tripID, now); !errors.Is(err, schedule.ErrNotScheduled) {
		t.Fatalf("dispatching twice: %v", err)
	}

	if err := store.Fail(ctx, due.ID, "late", now); !errors.Is(err, schedule.ErrNotScheduled) {
		t.Fatalf("failing a dispatched one: %v", err)
	}

	if err := store.Fail(ctx, later.ID, "the pickup is outside every service zone", now); err != nil {
		t.Fatal(err)
	}

	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'trip.schedule_failed' AND aggregate_id = $1`, later.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d %v", events, err)
	}

	failed, _, _ := store.FindByKey(ctx, later.RiderID, "later")
	if failed.Status != schedule.Failed || failed.LastError != "the pickup is outside every service zone" || failed.FailedAt == nil {
		t.Fatalf("failed %+v", failed)
	}

	done, _, _ := store.FindByKey(ctx, due.RiderID, "due")
	if done.Status != schedule.Dispatched || done.TripID != tripID {
		t.Fatalf("dispatched %+v", done)
	}
}

func TestATripKeepsItsPassengerAndItsBookingsID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	id := uuid.NewString()

	created, err := repo.Create(ctx, trip.CreateInput{
		ID: id, RiderID: uuid.NewString(),
		Pickup:        trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff:       trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		PassengerName: "Sara", PassengerPhone: "+9647500000002", Scheduled: true,
	})
	if err != nil || created.ID != id || created.PassengerName != "Sara" || created.PassengerPhone != "+9647500000002" || !created.Scheduled {
		t.Fatalf("created %+v %v", created, err)
	}

	// The same id again: never a second trip.
	if _, err := repo.Create(ctx, trip.CreateInput{
		ID: id, RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
	}); err == nil {
		t.Fatal("a second trip with the same id")
	}

	// A passenger's name without a phone is refused by the table too.
	if _, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:        trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff:       trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		PassengerName: "Sara",
	}); err == nil {
		t.Fatal("a name without a phone was stored")
	}
}
