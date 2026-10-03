package driver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// --- approval needs the documents ----------------------------------------------

func TestApproveDriver_RefusedWhileDocumentsAreIncomplete(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusPending}}
	compliance := &fakeCompliance{missing: []string{"driving_licence_front"}}
	svc := newServiceWithCompliance(repo, compliance)

	_, err := svc.ApproveDriver(context.Background(), "d1")
	if !errors.Is(err, driver.ErrDocumentsIncomplete) {
		t.Fatalf("got %v, want ErrDocumentsIncomplete", err)
	}

	if len(repo.updateStatusCalls) != 0 {
		t.Error("the driver was approved without their documents")
	}

	if len(compliance.purposes) != 1 || compliance.purposes[0] != driver.ForApproval {
		t.Errorf("checked for %v, want approval", compliance.purposes)
	}
}

func TestApproveDriver_AlreadyActiveIsReturnedWithoutChecks(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusActive}}
	compliance := &fakeCompliance{missing: []string{"profile_photo"}}
	svc := newServiceWithCompliance(repo, compliance)

	got, err := svc.ApproveDriver(context.Background(), "d1")
	if err != nil || got.Status != driver.StatusActive {
		t.Fatalf("got %v, %v; want the active driver", got.Status, err)
	}

	if compliance.calls != 0 || len(repo.updateStatusCalls) != 0 {
		t.Error("a retried approval checked or wrote something")
	}
}

func TestApproveDriver_SuspendedIsAnInvalidTransition(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusSuspended}}
	svc := newService(repo, "id")

	if _, err := svc.ApproveDriver(context.Background(), "d1"); !errors.Is(err, driver.ErrInvalidStatusTransition) {
		t.Fatalf("got %v, want ErrInvalidStatusTransition", err)
	}
}

func TestApproveDriver_ComplianceFailureIsNotAnApproval(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusPending}}
	svc := newServiceWithCompliance(repo, &fakeCompliance{err: errors.New("database down")})

	if _, err := svc.ApproveDriver(context.Background(), "d1"); err == nil {
		t.Fatal("approved although the documents could not be checked")
	}

	if len(repo.updateStatusCalls) != 0 {
		t.Error("the repository was written to")
	}
}

// --- going online needs the documents -----------------------------------------

func TestUpdateAvailability_IncompleteDocumentsCannotGoAvailable(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{
		ID: "d1", Status: driver.StatusActive, AvailabilityStatus: driver.AvailabilityOffline,
	}}
	compliance := &fakeCompliance{missing: []string{"vehicle_registration_front"}}
	svc := newServiceWithCompliance(repo, compliance)

	_, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
		DriverID: "d1", AvailabilityStatus: driver.AvailabilityAvailable,
	})
	if !errors.Is(err, driver.ErrDocumentsIncomplete) {
		t.Fatalf("got %v, want ErrDocumentsIncomplete", err)
	}

	if len(repo.updateAvailabilityCalls) != 0 {
		t.Error("the repository was written to")
	}

	if compliance.purposes[0] != driver.ForWork {
		t.Errorf("checked for %v, want work", compliance.purposes)
	}
}

func TestVehicleYear(t *testing.T) {
	repo := &fakeRepository{}
	svc := newService(repo, "id")

	in := validCreateInput()
	in.VehicleYear = 1975

	if _, err := svc.CreateDriver(context.Background(), in); !errors.Is(err, driver.ErrInvalidVehicleYear) {
		t.Fatalf("a 1975 car: got %v", err)
	}

	in.VehicleYear = 2019
	repo.findByIdentityIDErr = driver.ErrDriverNotFound

	if _, err := svc.CreateDriver(context.Background(), in); err != nil {
		t.Fatalf("a 2019 car: %v", err)
	}

	if got := repo.createCalls[0]; got.Vehicle.Year != 2019 || got.VehicleID == "" {
		t.Errorf("created with %+v", got)
	}
}

func TestUpdateAvailability_BusyDriverWithIncompleteDocumentsIsReleasedOffline(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{
		ID: "d1", Status: driver.StatusActive, AvailabilityStatus: driver.AvailabilityBusy,
	}}
	svc := newServiceWithCompliance(repo, &fakeCompliance{missing: []string{"driving_licence_front"}})

	if _, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
		DriverID: "d1", AvailabilityStatus: driver.AvailabilityAvailable,
	}); err != nil {
		t.Fatalf("release from a trip: %v", err)
	}

	if len(repo.updateAvailabilityCalls) != 1 || repo.updateAvailabilityCalls[0].AvailabilityStatus != driver.AvailabilityOffline {
		t.Fatalf("calls = %+v, want one write to offline", repo.updateAvailabilityCalls)
	}
}

func TestUpdateAvailability_BusyAndOfflineNeedNoDocumentCheck(t *testing.T) {
	for _, target := range []driver.AvailabilityStatus{driver.AvailabilityBusy, driver.AvailabilityOffline} {
		repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusActive}}
		compliance := &fakeCompliance{missing: []string{"profile_photo"}}
		svc := newServiceWithCompliance(repo, compliance)

		if _, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
			DriverID: "d1", AvailabilityStatus: target,
		}); err != nil {
			t.Errorf("going %q: %v", target, err)
		}

		if compliance.calls != 0 {
			t.Errorf("going %q checked the documents", target)
		}
	}
}

// --- an approved profile is locked ---------------------------------------------

func TestUpdateDriverProfile_ApprovedNameAndCarAreLocked(t *testing.T) {
	stored := driver.Driver{
		ID: "d1", Status: driver.StatusActive, DisplayName: "Ali",
		Vehicle: driver.Vehicle{Make: "Toyota", Model: "Camry", Color: "White", PlateNumber: "ABC-123", Class: driver.VehicleClassEconomy},
	}

	unchanged := driver.UpdateDriverProfileInput{
		DriverID: "d1", DisplayName: "Ali",
		VehicleMake: "Toyota", VehicleModel: "Camry", VehicleColor: "White", VehiclePlate: "abc-123",
	}

	changes := map[string]func(*driver.UpdateDriverProfileInput){
		"name":  func(in *driver.UpdateDriverProfileInput) { in.DisplayName = "Omar" },
		"plate": func(in *driver.UpdateDriverProfileInput) { in.VehiclePlate = "XYZ-999" },
		"color": func(in *driver.UpdateDriverProfileInput) { in.VehicleColor = "Black" },
		"class": func(in *driver.UpdateDriverProfileInput) { in.VehicleClass = "comfort" },
	}

	for name, change := range changes {
		repo := &fakeRepository{findByIDResult: stored}
		svc := newService(repo, "id")

		in := unchanged
		change(&in)

		if _, err := svc.UpdateDriverProfile(context.Background(), in); !errors.Is(err, driver.ErrProfileLocked) {
			t.Errorf("changing the %s: got %v, want ErrProfileLocked", name, err)
		}

		if len(repo.updateProfileCalls) != 0 {
			t.Errorf("changing the %s wrote to the repository", name)
		}
	}

	repo := &fakeRepository{findByIDResult: stored, updateProfileResult: stored}
	svc := newService(repo, "id")

	if _, err := svc.UpdateDriverProfile(context.Background(), unchanged); err != nil {
		t.Fatalf("sending the profile unchanged: %v", err)
	}
}

func TestUpdateDriverProfile_PendingDriverMayChangeEverything(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusPending, DisplayName: "Ali"}}
	svc := newService(repo, "id")

	if _, err := svc.UpdateDriverProfile(context.Background(), driver.UpdateDriverProfileInput{
		DriverID: "d1", DisplayName: "Omar",
		VehicleMake: "Kia", VehicleModel: "Rio", VehicleColor: "Red", VehiclePlate: "NEW-1",
	}); err != nil {
		t.Fatalf("a pending driver fixing their profile: %v", err)
	}
}
