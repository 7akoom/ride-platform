package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/vehicles"
)

func TestVehicles(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	drivers := NewDriverRepository(pool)
	repo := NewVehicleRepository(pool)
	docs := NewDocumentRepository(pool)

	firstCar := uuid.NewString()
	created, err := drivers.Create(ctx, driver.CreateInput{
		ID: uuid.NewString(), VehicleID: firstCar, IdentityID: uuid.NewString(), DisplayName: "Car Owner",
		Vehicle: driver.Vehicle{Make: "Kia", Model: "Rio", Color: "Red", PlateNumber: "CAR-1", Class: driver.VehicleClassEconomy, Year: 2019},
	})
	if err != nil {
		t.Fatal(err)
	}

	if created.Vehicle.ID != firstCar || created.Vehicle.Year != 2019 {
		t.Fatalf("driver's car copy: %+v", created.Vehicle)
	}

	active, found, err := repo.ActiveVehicle(ctx, created.ID)
	if err != nil || !found || active.ID != firstCar || active.Status != "pending" {
		t.Fatalf("first car: %+v %v %v", active, found, err)
	}

	// A pending driver fixing the car fixes the first car too.
	if _, err := drivers.UpdateProfile(ctx, driver.UpdateProfileInput{
		DriverID: created.ID, DisplayName: "Car Owner",
		Vehicle: driver.Vehicle{Make: "Kia", Model: "Rio", Color: "Blue", PlateNumber: "CAR-1B"},
	}); err != nil {
		t.Fatal(err)
	}

	first, _ := repo.Get(ctx, firstCar)
	if first.PlateNumber != "CAR-1B" || first.Color != "Blue" || first.Year != 2019 {
		t.Errorf("first car after the profile change: %+v", first)
	}

	// Approving the driver approves the first car.
	if _, err := drivers.UpdateStatus(ctx, driver.UpdateStatusInput{
		DriverID: created.ID, To: driver.StatusActive, AllowedFrom: []driver.Status{driver.StatusPending},
	}); err != nil {
		t.Fatal(err)
	}

	if first, _ = repo.Get(ctx, firstCar); first.Status != vehicles.StatusApproved {
		t.Errorf("first car after the driver's approval: %s", first.Status)
	}

	second := vehicles.Vehicle{ID: uuid.NewString(), DriverID: created.ID, Make: "Toyota", Model: "Camry", Color: "White", PlateNumber: "CAR-2", Year: 2022, Class: "comfort"}
	if _, err := repo.Add(ctx, second, 5); err != nil {
		t.Fatal(err)
	}

	taken := second
	taken.ID = uuid.NewString()
	if _, err := repo.Add(ctx, taken, 5); !errors.Is(err, vehicles.ErrPlateTaken) {
		t.Errorf("same plate twice: %v", err)
	}

	if _, err := repo.Add(ctx, vehicles.Vehicle{ID: uuid.NewString(), DriverID: created.ID, Make: "X", Model: "Y", PlateNumber: "CAR-3", Year: 2020, Class: "economy"}, 2); !errors.Is(err, vehicles.ErrTooManyVehicles) {
		t.Errorf("over the limit: %v", err)
	}

	if _, err := repo.Activate(ctx, created.ID, second.ID); !errors.Is(err, vehicles.ErrVehicleNotApproved) {
		t.Errorf("activating a pending car: %v", err)
	}

	// A document for the new car does not clash with the first car's.
	regType, _ := docs.GetType(ctx, "vehicle_registration_front")
	for _, car := range []string{firstCar, second.ID} {
		d := newDoc(created.ID, regType.Code, "REG-"+car[:8], documents.NewDate(2030, 1, 1))
		d.VehicleID = car

		if _, _, err := docs.Submit(ctx, d); err != nil {
			t.Fatalf("registration for %s: %v", car, err)
		}

		if _, _, err := docs.Approve(ctx, documents.ApproveRecord{DocumentID: d.ID, Number: d.Number, ExpiresOn: d.ExpiresOn, Type: regType}); err != nil {
			t.Fatalf("approving the registration of %s: %v", car, err)
		}
	}

	if _, err := repo.Approve(ctx, vehicles.ApproveRecord{VehicleID: second.ID, Year: 2022, Class: "comfort"}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Approve(ctx, vehicles.ApproveRecord{VehicleID: second.ID, Year: 2022, Class: "comfort"}); !errors.Is(err, vehicles.ErrVehicleNotPending) {
		t.Errorf("approving twice: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE drivers SET availability_status = 'available' WHERE id = $1`, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Activate(ctx, created.ID, second.ID); !errors.Is(err, vehicles.ErrDriverNotOffline) {
		t.Errorf("changing cars while online: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE drivers SET availability_status = 'offline' WHERE id = $1`, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Activate(ctx, created.ID, second.ID); err != nil {
		t.Fatal(err)
	}

	after, _ := drivers.FindByID(ctx, created.ID)
	if after.Vehicle.ID != second.ID || after.Vehicle.PlateNumber != "CAR-2" || after.Vehicle.Class != driver.VehicleClassComfort || after.Vehicle.Year != 2022 {
		t.Errorf("the driver's car copy after the change: %+v", after.Vehicle)
	}

	if _, _, err := repo.Retire(ctx, created.ID, second.ID); !errors.Is(err, vehicles.ErrVehicleActive) {
		t.Errorf("retiring the active car: %v", err)
	}

	_, files, err := repo.Retire(ctx, created.ID, firstCar)
	if err != nil || len(files) != 1 {
		t.Fatalf("retiring the first car: %v %v", files, err)
	}

	// The retired car's plate is free again, for another driver.
	other := addDriver(t, pool, "offline")
	if _, err := repo.Add(ctx, vehicles.Vehicle{ID: uuid.NewString(), DriverID: other, Make: "Kia", Model: "Rio", PlateNumber: "CAR-1B", Year: 2019, Class: "economy"}, 5); err != nil {
		t.Errorf("a sold car for its new owner: %v", err)
	}

	list, err := repo.ListByDriver(ctx, created.ID)
	if err != nil || len(list) != 2 || list[0].ID != second.ID || list[1].Status != vehicles.StatusRetired {
		t.Errorf("list: %+v %v", list, err)
	}

	if got := events(t, pool, created.ID, "driver.vehicle_reviewed"); len(got) != 1 {
		t.Errorf("review events: %v", got)
	}
}
