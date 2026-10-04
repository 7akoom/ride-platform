package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/erasure"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestErasureStoreErasesWhatTripsSayAboutThePerson(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	riderID, driverID, otherRider := uuid.NewString(), uuid.NewString(), uuid.NewString()

	create := func(rider string) trip.Trip {
		created, err := repo.Create(ctx, trip.CreateInput{
			ID: uuid.NewString(), RiderID: rider,
			Pickup:        trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
			Dropoff:       trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
			PickupAddress: "Home", DropoffAddress: "Mall",
			PickupDetails: "Floor 2", PickupNote: "Blue gate", PickupPhotoMediaID: uuid.NewString(),
			PassengerName: "Sara", PassengerPhone: "+9647501111111",
		})
		if err != nil {
			t.Fatal(err)
		}

		return created
	}

	mine, theirs := create(riderID), create(otherRider)

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	exec(`INSERT INTO trip_ratings (trip_id, rated_by, rater_id, ratee_id, stars, comment) VALUES ($1, 'rider', $2, $3, 5, 'great')`, mine.ID, riderID, driverID)
	exec(`INSERT INTO trip_ratings (trip_id, rated_by, rater_id, ratee_id, stars, comment) VALUES ($1, 'driver', $2, $3, 4, 'polite')`, mine.ID, driverID, riderID)
	exec(`INSERT INTO trip_shares (trip_id, token_hash, created_at, expires_at) VALUES ($1, decode(repeat('ab', 32), 'hex'), now(), now() + interval '1 hour')`, mine.ID)
	exec(`INSERT INTO scheduled_trips (id, rider_id, idempotency_key, scheduled_at, pickup_latitude, pickup_longitude,
	          dropoff_latitude, dropoff_longitude, vehicle_class, payment_method, passenger_name, passenger_phone, next_attempt_at)
	      VALUES (gen_random_uuid(), $1, 'k1', now() + interval '1 day', 36.1, 44.1, 36.2, 44.2, 'economy', 'cash', 'Sara', '+9647501111111', now() + interval '1 day')`, riderID)

	store := NewErasureStore(pool)

	for i := 0; i < 2; i++ {
		if err := store.Erase(ctx, erasure.Account{IdentityID: uuid.NewString(), RiderID: riderID}); err != nil {
			t.Fatal(err)
		}
	}

	read := func(id string) trip.Trip {
		found, err := repo.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		return found
	}

	if got := read(mine.ID); got.PassengerName != "" || got.PassengerPhone != "" || got.PickupDetails != "" ||
		got.PickupNote != "" || got.PickupPhotoMediaID != "" || got.PickupAddress != "Home" {
		t.Fatalf("the rider's trip: %+v", got)
	}

	if got := read(theirs.ID); got.PassengerName != "Sara" || got.PickupNote != "Blue gate" {
		t.Fatalf("another rider's trip was erased: %+v", got)
	}

	count := func(query string, args ...any) int {
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	if count(`SELECT count(*) FROM trip_ratings WHERE rater_id = $1 AND comment IS NOT NULL`, riderID) != 0 {
		t.Fatal("the rider's comment is still there")
	}

	if count(`SELECT count(*) FROM trip_ratings WHERE rater_id = $1 AND comment IS NOT NULL`, driverID) != 1 {
		t.Fatal("the driver's comment about the rider went too")
	}

	if count(`SELECT count(*) FROM trip_shares WHERE trip_id = $1 AND revoked_at IS NULL`, mine.ID) != 0 {
		t.Fatal("the share link still works")
	}

	if count(`SELECT count(*) FROM scheduled_trips WHERE rider_id = $1 AND (status <> 'cancelled' OR passenger_name <> '')`, riderID) != 0 {
		t.Fatal("the booking is still open or still names the passenger")
	}

	if err := store.Erase(ctx, erasure.Account{IdentityID: uuid.NewString(), DriverID: driverID}); err != nil {
		t.Fatal(err)
	}

	if count(`SELECT count(*) FROM trip_ratings WHERE rater_id = $1 AND comment IS NOT NULL`, driverID) != 0 {
		t.Fatal("the driver's comment is still there")
	}
}
