package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

var stopsOnTheWay = []trip.Stop{
	{Coordinates: trip.Coordinates{Latitude: 36.15, Longitude: 44.15}, Address: "Bakery"},
	{Coordinates: trip.Coordinates{Latitude: 36.17, Longitude: 44.17}, Address: "Pharmacy"},
}

func TestATripsStopsAreKeptAndReachedOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)

	created, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		Stops:   stopsOnTheWay,
	})
	if err != nil || len(created.Stops) != 2 || created.Stops[1].Address != "Pharmacy" || created.Stops[0].ReachedAt != nil {
		t.Fatalf("created %+v %v", created.Stops, err)
	}

	if _, err := repo.MarkStopReached(ctx, created.ID, 1); !errors.Is(err, trip.ErrInvalidTransition) {
		t.Fatalf("before the trip started: %v", err)
	}

	if _, err := repo.Accept(ctx, created.ID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Start(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.MarkStopReached(ctx, created.ID, 3); !errors.Is(err, trip.ErrStopNotFound) {
		t.Fatalf("a third stop: %v", err)
	}

	reached, err := repo.MarkStopReached(ctx, created.ID, 2)
	if err != nil || reached.Stops[1].ReachedAt == nil || reached.Stops[0].ReachedAt != nil {
		t.Fatalf("reached %+v %v", reached.Stops, err)
	}

	if time.Since(*reached.Stops[1].ReachedAt) > time.Minute {
		t.Fatalf("reached at %v", reached.Stops[1].ReachedAt)
	}

	again, err := repo.MarkStopReached(ctx, created.ID, 2)
	if err != nil || !again.Stops[1].ReachedAt.Equal(*reached.Stops[1].ReachedAt) {
		t.Fatalf("again %+v %v", again.Stops, err)
	}

	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'trip.stop_reached' AND aggregate_id = $1`, created.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d %v", events, err)
	}

	read, err := repo.FindByID(ctx, created.ID)
	if err != nil || read.Stops[1].ReachedAt == nil || read.Stops[0].Address != "Bakery" {
		t.Fatalf("read %+v %v", read.Stops, err)
	}

	// The table refuses a third stop even if a caller forgot the check.
	if _, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		Stops:   append(append([]trip.Stop{}, stopsOnTheWay...), stopsOnTheWay[0]),
	}); err == nil {
		t.Fatal("a trip with three stops was stored")
	}
}

func TestABookingKeepsItsStops(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewScheduleStore(pool)

	ride := booking(uuid.NewString(), "stops", time.Now().Add(2*time.Hour))
	ride.Stops = stopsOnTheWay

	created, _, err := store.Create(ctx, ride, 3)
	if err != nil || len(created.Stops) != 2 || created.Stops[0].Address != "Bakery" {
		t.Fatalf("created %+v %v", created.Stops, err)
	}

	listed, err := store.ListForRider(ctx, ride.RiderID, false, 50)
	if err != nil || len(listed) != 1 || len(listed[0].Stops) != 2 {
		t.Fatalf("listed %+v %v", listed, err)
	}
}
