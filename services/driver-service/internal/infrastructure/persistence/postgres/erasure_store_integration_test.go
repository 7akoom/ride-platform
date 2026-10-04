package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/erasure"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/profile"
)

func TestErasureStoreErasesTheDriver(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	driverID := addDriver(t, pool, "available")

	var identityID, plate string
	if err := pool.QueryRow(ctx, `SELECT identity_id::text, vehicle_plate_number FROM drivers WHERE id = $1`, driverID).Scan(&identityID, &plate); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO vehicles (id, driver_id, make, model, color, plate_number, vehicle_class, status, active)
		 VALUES (gen_random_uuid(), $1, 'Kia', 'Rio', 'Red', $2, 'economy', 'approved', true)`, driverID, plate); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO driver_documents (id, driver_id, type_code, media_id, document_number, status)
		 VALUES (gen_random_uuid(), $1, 'driving_licence_front', $2, 'L-123456', 'approved')`, driverID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}

	repo := NewProfileRepository(pool)
	if _, err := repo.SaveFields(ctx, driverID, profile.Fields{Nationality: "IQ"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CreateNameChange(ctx, profile.NameChange{
		ID: uuid.NewString(), DriverID: driverID, CurrentName: "Test Driver", RequestedName: "Ali",
		Status: profile.NameChangePending, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	store := NewErasureStore(pool)
	account := erasure.Account{IdentityID: identityID, DriverID: driverID}

	if err := store.Requested(ctx, account); err != nil {
		t.Fatal(err)
	}

	var availability string
	if err := pool.QueryRow(ctx, `SELECT availability_status FROM drivers WHERE id = $1`, driverID).Scan(&availability); err != nil || availability != "offline" {
		t.Fatalf("after the request the driver is %q (%v)", availability, err)
	}

	for i := 0; i < 2; i++ {
		if err := store.Erase(ctx, account); err != nil {
			t.Fatal(err)
		}
	}

	var name, status, newPlate string
	if err := pool.QueryRow(ctx, `SELECT display_name, status, vehicle_plate_number FROM drivers WHERE id = $1`, driverID).
		Scan(&name, &status, &newPlate); err != nil || name != DeletedDriverName || status != "suspended" || !strings.HasPrefix(newPlate, "DELETED-") {
		t.Fatalf("driver %q %q %q %v", name, status, newPlate, err)
	}

	count := func(query string) int {
		var n int
		if err := pool.QueryRow(ctx, query, driverID).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	if count(`SELECT count(*) FROM driver_documents WHERE driver_id = $1`) != 0 ||
		count(`SELECT count(*) FROM driver_details WHERE driver_id = $1`) != 0 ||
		count(`SELECT count(*) FROM driver_name_changes WHERE driver_id = $1`) != 0 {
		t.Fatal("documents, details or name changes are still there")
	}

	if count(`SELECT count(*) FROM vehicles WHERE driver_id = $1 AND (status <> 'retired' OR active OR plate_number NOT LIKE 'DELETED-%')`) != 0 {
		t.Fatal("a car still has its plate or is in service")
	}

	// The plate is free for someone else.
	other := addDriver(t, pool, "offline")
	if _, err := pool.Exec(ctx, `UPDATE drivers SET vehicle_plate_number = $2 WHERE id = $1`, other, plate); err != nil {
		t.Fatalf("the plate was not freed: %v", err)
	}
}
