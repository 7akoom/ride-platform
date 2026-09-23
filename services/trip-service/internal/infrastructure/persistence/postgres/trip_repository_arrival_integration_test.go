package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestArrivalAndWhoCancelled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)

	newTrip := func() trip.Trip {
		t.Helper()

		created, err := repo.Create(ctx, trip.CreateInput{
			ID: uuid.NewString(), RiderID: uuid.NewString(),
			Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
			Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		})
		if err != nil {
			t.Fatal(err)
		}

		return created
	}

	requested := newTrip()
	if _, err := repo.MarkArrived(ctx, requested.ID); !errors.Is(err, trip.ErrInvalidTransition) {
		t.Fatalf("a requested trip: %v", err)
	}

	if _, err := repo.MarkArrived(ctx, uuid.NewString()); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("unknown trip: %v", err)
	}

	accepted, err := repo.Accept(ctx, requested.ID, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}

	arrived, err := repo.MarkArrived(ctx, accepted.ID)
	if err != nil || arrived.ArrivedAt == nil {
		t.Fatalf("arrived %+v %v", arrived, err)
	}

	again, err := repo.MarkArrived(ctx, accepted.ID)
	if err != nil || !again.ArrivedAt.Equal(*arrived.ArrivedAt) {
		t.Fatalf("again %+v %v", again, err)
	}

	var arrivals int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'trip.driver_arrived' AND aggregate_id = $1`, accepted.ID).Scan(&arrivals); err != nil || arrivals != 1 {
		t.Fatalf("arrival events %d %v", arrivals, err)
	}

	refused := errors.New("not yet")
	if _, err := repo.Cancel(ctx, trip.CancelRecord{TripID: accepted.ID, By: trip.CancelledByDriver, RiderNoShow: true,
		Allow: func(current trip.Trip) error {
			if current.ArrivedAt == nil {
				t.Error("Allow must see the arrival")
			}
			return refused
		}}); !errors.Is(err, refused) {
		t.Fatalf("refused: %v", err)
	}

	cancelled, err := repo.Cancel(ctx, trip.CancelRecord{TripID: accepted.ID, Reason: "no show", By: trip.CancelledByDriver, RiderNoShow: true})
	if err != nil || cancelled.CancelledBy != trip.CancelledByDriver || !cancelled.RiderNoShow || cancelled.Status != trip.StatusCancelled {
		t.Fatalf("cancelled %+v %v", cancelled, err)
	}

	var payload string
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM outbox_events WHERE event_type = 'trip.cancelled' AND aggregate_id = $1`, accepted.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(payload, `"cancelled_by": "driver"`) || !strings.Contains(payload, `"rider_no_show": "true"`) {
		t.Fatalf("payload %s", payload)
	}

	// Only the driver reports a no-show, even straight in the table.
	other := newTrip()
	if _, err := repo.Cancel(ctx, trip.CancelRecord{TripID: other.ID, By: trip.CancelledByRider, RiderNoShow: true}); err == nil {
		t.Fatal("a rider no-show by the rider was stored")
	}
}
