package driver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	findByIdentityIDResult driver.Driver
	findByIdentityIDErr    error

	createResult driver.Driver
	createErr    error
	createCalls  []driver.CreateInput

	findByIDResult driver.Driver
	findByIDErr    error

	updateProfileResult driver.Driver
	updateProfileErr    error
	updateProfileCalls  []driver.UpdateProfileInput

	updateAvailabilityResult driver.Driver
	updateAvailabilityErr    error
	updateAvailabilityCalls  []driver.UpdateAvailabilityInput

	updateStatusResult driver.Driver
	updateStatusErr    error
	updateStatusCalls  []driver.UpdateStatusInput
}

func (r *fakeRepository) Create(
	_ context.Context,
	input driver.CreateInput,
) (driver.Driver, error) {
	r.createCalls = append(r.createCalls, input)

	if r.createErr != nil {
		return driver.Driver{}, r.createErr
	}

	return r.createResult, nil
}

func (r *fakeRepository) FindByID(
	_ context.Context,
	_ string,
) (driver.Driver, error) {
	if r.findByIDErr != nil {
		return driver.Driver{}, r.findByIDErr
	}

	return r.findByIDResult, nil
}

func (r *fakeRepository) FindByIdentityID(
	_ context.Context,
	_ string,
) (driver.Driver, error) {
	if r.findByIdentityIDErr != nil {
		return driver.Driver{}, r.findByIdentityIDErr
	}

	return r.findByIdentityIDResult, nil
}

func (r *fakeRepository) UpdateProfile(
	_ context.Context,
	input driver.UpdateProfileInput,
) (driver.Driver, error) {
	r.updateProfileCalls = append(r.updateProfileCalls, input)

	if r.updateProfileErr != nil {
		return driver.Driver{}, r.updateProfileErr
	}

	return r.updateProfileResult, nil
}

func (r *fakeRepository) UpdateAvailability(
	_ context.Context,
	input driver.UpdateAvailabilityInput,
) (driver.Driver, error) {
	r.updateAvailabilityCalls = append(r.updateAvailabilityCalls, input)

	if r.updateAvailabilityErr != nil {
		return driver.Driver{}, r.updateAvailabilityErr
	}

	return r.updateAvailabilityResult, nil
}

type fakeIDGenerator struct{ id string }

func (g *fakeIDGenerator) NewID() string { return g.id }

func newService(repo *fakeRepository, id string) driver.Service {
	return driver.NewService(repo, &fakeIDGenerator{id: id})
}

func validCreateInput() driver.CreateDriverInput {
	return driver.CreateDriverInput{
		IdentityID:   "identity-1",
		DisplayName:  "Ali",
		VehicleMake:  "Toyota",
		VehicleModel: "Camry",
		VehicleColor: "White",
		VehiclePlate: "abc-123",
	}
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnMissingDependencies(t *testing.T) {
	t.Run("nil repository", func(t *testing.T) {
		defer expectPanic(t)
		driver.NewService(nil, &fakeIDGenerator{id: "x"})
	})

	t.Run("nil id generator", func(t *testing.T) {
		defer expectPanic(t)
		driver.NewService(&fakeRepository{}, nil)
	})
}

func expectPanic(t *testing.T) {
	t.Helper()

	if recover() == nil {
		t.Fatal("expected a panic")
	}
}

// --- CreateDriver ---------------------------------------------------------

func TestService_CreateDriver_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(driver.CreateDriverInput) driver.CreateDriverInput
		wantErr error
	}{
		{"empty identity id", func(i driver.CreateDriverInput) driver.CreateDriverInput {
			i.IdentityID = "  "
			return i
		}, driver.ErrIdentityIDRequired},
		{"empty display name", func(i driver.CreateDriverInput) driver.CreateDriverInput {
			i.DisplayName = ""
			return i
		}, driver.ErrDisplayNameRequired},
		{"display name too long", func(i driver.CreateDriverInput) driver.CreateDriverInput {
			i.DisplayName = strings.Repeat("a", 121)
			return i
		}, driver.ErrDisplayNameTooLong},
		{"missing vehicle make", func(i driver.CreateDriverInput) driver.CreateDriverInput {
			i.VehicleMake = ""
			return i
		}, driver.ErrVehicleFieldsRequired},
		{"missing plate", func(i driver.CreateDriverInput) driver.CreateDriverInput {
			i.VehiclePlate = "  "
			return i
		}, driver.ErrVehicleFieldsRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{findByIdentityIDErr: driver.ErrDriverNotFound}
			svc := newService(repo, "new-id")

			_, err := svc.CreateDriver(context.Background(), tc.mutate(validCreateInput()))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.createCalls) != 0 {
				t.Fatal("expected Create not to be called on validation failure")
			}
		})
	}
}

func TestService_CreateDriver_AlreadyExistsForIdentity(t *testing.T) {
	repo := &fakeRepository{findByIdentityIDResult: driver.Driver{ID: "existing"}}
	svc := newService(repo, "new-id")

	_, err := svc.CreateDriver(context.Background(), validCreateInput())
	if !errors.Is(err, driver.ErrDriverAlreadyExists) {
		t.Fatalf("got %v, want ErrDriverAlreadyExists", err)
	}
}

func TestService_CreateDriver_WrapsUnexpectedLookupError(t *testing.T) {
	lookupErr := errors.New("connection reset")
	repo := &fakeRepository{findByIdentityIDErr: lookupErr}
	svc := newService(repo, "new-id")

	_, err := svc.CreateDriver(context.Background(), validCreateInput())
	if !errors.Is(err, lookupErr) {
		t.Fatalf("got %v, want wrapped %v", err, lookupErr)
	}
}

func TestService_CreateDriver_WrapsRepositoryCreateError(t *testing.T) {
	createErr := errors.New("unique violation")
	repo := &fakeRepository{
		findByIdentityIDErr: driver.ErrDriverNotFound,
		createErr:           createErr,
	}
	svc := newService(repo, "new-id")

	_, err := svc.CreateDriver(context.Background(), validCreateInput())
	if !errors.Is(err, createErr) {
		t.Fatalf("got %v, want wrapped %v", err, createErr)
	}
}

func TestService_CreateDriver_HappyPath(t *testing.T) {
	repo := &fakeRepository{
		findByIdentityIDErr: driver.ErrDriverNotFound,
		createResult:        driver.Driver{ID: "new-id"},
	}
	svc := newService(repo, "new-id")

	got, err := svc.CreateDriver(context.Background(), validCreateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "new-id" {
		t.Fatalf("got %+v", got)
	}

	if len(repo.createCalls) != 1 {
		t.Fatalf("expected 1 Create call, got %d", len(repo.createCalls))
	}

	call := repo.createCalls[0]
	if call.Vehicle.PlateNumber != "ABC-123" {
		t.Fatalf("expected plate to be normalized to upper case, got %q", call.Vehicle.PlateNumber)
	}
}

// --- GetDriver / GetDriverByIdentityID -----------------------------------

func TestService_GetDriver_EmptyID(t *testing.T) {
	svc := newService(&fakeRepository{}, "id")

	_, err := svc.GetDriver(context.Background(), " ")
	if !errors.Is(err, driver.ErrDriverIDRequired) {
		t.Fatalf("got %v, want ErrDriverIDRequired", err)
	}
}

func TestService_GetDriver_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("not found")
	repo := &fakeRepository{findByIDErr: repoErr}
	svc := newService(repo, "id")

	_, err := svc.GetDriver(context.Background(), "driver-1")
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

func TestService_GetDriverByIdentityID_EmptyID(t *testing.T) {
	svc := newService(&fakeRepository{}, "id")

	_, err := svc.GetDriverByIdentityID(context.Background(), "")
	if !errors.Is(err, driver.ErrIdentityIDRequired) {
		t.Fatalf("got %v, want ErrIdentityIDRequired", err)
	}
}

// --- UpdateDriverProfile ---------------------------------------------------

func TestService_UpdateDriverProfile_ValidationErrors(t *testing.T) {
	valid := driver.UpdateDriverProfileInput{
		DriverID:     "driver-1",
		DisplayName:  "Ali",
		VehicleMake:  "Toyota",
		VehicleModel: "Camry",
		VehiclePlate: "abc-123",
	}

	cases := []struct {
		name    string
		mutate  func(driver.UpdateDriverProfileInput) driver.UpdateDriverProfileInput
		wantErr error
	}{
		{"empty driver id", func(i driver.UpdateDriverProfileInput) driver.UpdateDriverProfileInput {
			i.DriverID = ""
			return i
		}, driver.ErrDriverIDRequired},
		{"empty display name", func(i driver.UpdateDriverProfileInput) driver.UpdateDriverProfileInput {
			i.DisplayName = ""
			return i
		}, driver.ErrDisplayNameRequired},
		{"missing vehicle fields", func(i driver.UpdateDriverProfileInput) driver.UpdateDriverProfileInput {
			i.VehiclePlate = ""
			return i
		}, driver.ErrVehicleFieldsRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo, "id")

			_, err := svc.UpdateDriverProfile(context.Background(), tc.mutate(valid))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.updateProfileCalls) != 0 {
				t.Fatal("expected UpdateProfile not to be called on validation failure")
			}
		})
	}
}

func TestService_UpdateDriverProfile_HappyPath(t *testing.T) {
	repo := &fakeRepository{updateProfileResult: driver.Driver{ID: "driver-1"}}
	svc := newService(repo, "id")

	got, err := svc.UpdateDriverProfile(context.Background(), driver.UpdateDriverProfileInput{
		DriverID:     "driver-1",
		DisplayName:  "New Name",
		VehicleMake:  "Toyota",
		VehicleModel: "Camry",
		VehiclePlate: "xyz-999",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "driver-1" {
		t.Fatalf("got %+v", got)
	}
}

// --- UpdateAvailability -----------------------------------------------------

func TestService_UpdateAvailability_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   driver.UpdateDriverAvailabilityInput
		wantErr error
	}{
		{"empty driver id", driver.UpdateDriverAvailabilityInput{DriverID: "", AvailabilityStatus: driver.AvailabilityAvailable}, driver.ErrDriverIDRequired},
		{"invalid status", driver.UpdateDriverAvailabilityInput{DriverID: "driver-1", AvailabilityStatus: "on_break"}, driver.ErrInvalidAvailability},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo, "id")

			_, err := svc.UpdateAvailability(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.updateAvailabilityCalls) != 0 {
				t.Fatal("expected UpdateAvailability not to be called on validation failure")
			}
		})
	}
}

func TestService_UpdateAvailability_AcceptsEveryValidStatus(t *testing.T) {
	for _, status := range []driver.AvailabilityStatus{
		driver.AvailabilityOffline,
		driver.AvailabilityAvailable,
		driver.AvailabilityBusy,
	} {
		t.Run(string(status), func(t *testing.T) {
			repo := &fakeRepository{updateAvailabilityResult: driver.Driver{ID: "driver-1", AvailabilityStatus: status}}
			svc := newService(repo, "id")

			got, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
				DriverID:           "driver-1",
				AvailabilityStatus: status,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.AvailabilityStatus != status {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestService_UpdateAvailability_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("row locked")
	repo := &fakeRepository{updateAvailabilityErr: repoErr}
	svc := newService(repo, "id")

	_, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
		DriverID:           "driver-1",
		AvailabilityStatus: driver.AvailabilityAvailable,
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

// --- Vehicle value object ---------------------------------------------------

func TestNewVehicle(t *testing.T) {
	cases := []struct {
		name      string
		make_     string
		model     string
		color     string
		plate     string
		wantErr   error
		wantPlate string
	}{
		{"valid", "Toyota", "Camry", "White", "abc-123", nil, "ABC-123"},
		{"missing make", "", "Camry", "White", "abc-123", driver.ErrVehicleFieldsRequired, ""},
		{"missing model", "Toyota", "", "White", "abc-123", driver.ErrVehicleFieldsRequired, ""},
		{"missing plate", "Toyota", "Camry", "White", "  ", driver.ErrVehicleFieldsRequired, ""},
		{"color optional", "Toyota", "Camry", "", "abc-123", nil, "ABC-123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := driver.NewVehicle(tc.make_, tc.model, tc.color, tc.plate, "")

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got error %v, want %v", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.PlateNumber != tc.wantPlate {
				t.Fatalf("got plate %q, want %q", got.PlateNumber, tc.wantPlate)
			}
		})
	}
}

func (r *fakeRepository) UpdateStatus(
	_ context.Context,
	input driver.UpdateStatusInput,
) (driver.Driver, error) {
	r.updateStatusCalls = append(r.updateStatusCalls, input)

	if r.updateStatusErr != nil {
		return driver.Driver{}, r.updateStatusErr
	}

	return r.updateStatusResult, nil
}
