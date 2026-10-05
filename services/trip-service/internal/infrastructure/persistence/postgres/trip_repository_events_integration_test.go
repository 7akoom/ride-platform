package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestTripRequestedSaysWhereAndWhat(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	zone, city := uuid.NewString(), uuid.NewString()

	created, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:        trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff:       trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		VehicleClass:  "comfort",
		PaymentMethod: "wallet",
		Scheduled:     true,
		PickupZoneID:  zone,
		PickupCityID:  city,
		PickupAddress: "Home", PassengerName: "Sara", PassengerPhone: "+9647501111111",
	})
	if err != nil {
		t.Fatal(err)
	}

	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE event_type = 'trip.requested' AND aggregate_id = $1`, created.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}

	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"trip_id": created.ID, "rider_id": created.RiderID, "city_id": city, "zone_id": zone,
		"vehicle_class": "comfort", "payment_method": "wallet", "scheduled": "true",
	}

	if len(payload) != len(want) {
		t.Fatalf("payload %v, want exactly %v (nothing about the person or the places)", payload, want)
	}

	for key, value := range want {
		if payload[key] != value {
			t.Errorf("%s = %q, want %q", key, payload[key], value)
		}
	}
}
