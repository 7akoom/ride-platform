package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/erasure"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/profile"
)

func TestErasureStoreErasesTheRider(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	riderID, identityID := addRider(t, pool)
	otherID, _ := addRider(t, pool)

	for _, id := range []string{riderID, otherID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO saved_addresses (id, rider_id, kind, label, latitude, longitude)
			 VALUES (gen_random_uuid(), $1, 'other', 'Gym', 36.1, 44.1)`, id); err != nil {
			t.Fatal(err)
		}

		if _, err := NewDetailsRepository(pool).SaveFields(ctx, id, profile.Fields{Gender: "male"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	store := NewErasureStore(pool)

	for i := 0; i < 2; i++ { // safe to repeat
		if err := store.Erase(ctx, erasure.Account{IdentityID: identityID, RiderID: riderID}); err != nil {
			t.Fatal(err)
		}
	}

	var name, status string
	if err := pool.QueryRow(ctx, `SELECT display_name, status FROM riders WHERE id = $1`, riderID).Scan(&name, &status); err != nil ||
		name != DeletedRiderName || status != "suspended" {
		t.Fatalf("rider %q %q %v", name, status, err)
	}

	count := func(query, id string) int {
		var n int
		if err := pool.QueryRow(ctx, query, id).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	if count(`SELECT count(*) FROM saved_addresses WHERE rider_id = $1`, riderID) != 0 ||
		count(`SELECT count(*) FROM rider_details WHERE rider_id = $1`, riderID) != 0 {
		t.Fatal("the rider's addresses or details are still there")
	}

	if count(`SELECT count(*) FROM saved_addresses WHERE rider_id = $1`, otherID) != 1 ||
		count(`SELECT count(*) FROM rider_details WHERE rider_id = $1`, otherID) != 1 {
		t.Fatal("another rider's data was erased")
	}
}
