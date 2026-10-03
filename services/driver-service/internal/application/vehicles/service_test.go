package vehicles_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/vehicles"
)

const (
	driverID  = "0b0e0000-0000-4000-8000-000000000001"
	otherID   = "0b0e0000-0000-4000-8000-000000000002"
	vehicleID = "ca000000-0000-4000-8000-000000000001"
	staffID   = "5a000000-0000-4000-8000-000000000001"
)

type fakeRepo struct {
	stored    map[string]vehicles.Vehicle
	added     []vehicles.Vehicle
	updated   []vehicles.Vehicle
	approved  []vehicles.ApproveRecord
	rejected  []vehicles.RejectRecord
	activated []string
	retired   []string
	files     []string
	addErr    error
}

func (r *fakeRepo) Add(_ context.Context, v vehicles.Vehicle, max int) (vehicles.Vehicle, error) {
	if max != vehicles.MaxInService {
		return vehicles.Vehicle{}, errors.New("wrong limit")
	}

	r.added = append(r.added, v)

	return v, r.addErr
}

func (r *fakeRepo) Get(_ context.Context, id string) (vehicles.Vehicle, error) {
	v, ok := r.stored[id]
	if !ok {
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotFound
	}

	return v, nil
}

func (r *fakeRepo) ListByDriver(context.Context, string) ([]vehicles.Vehicle, error) { return nil, nil }

func (r *fakeRepo) Update(_ context.Context, v vehicles.Vehicle) (vehicles.Vehicle, error) {
	r.updated = append(r.updated, v)

	return v, nil
}

func (r *fakeRepo) Activate(_ context.Context, d, v string) (vehicles.Vehicle, error) {
	r.activated = append(r.activated, d+"/"+v)

	return vehicles.Vehicle{ID: v, Active: true}, nil
}

func (r *fakeRepo) Retire(_ context.Context, d, v string) (vehicles.Vehicle, []string, error) {
	r.retired = append(r.retired, d+"/"+v)

	return vehicles.Vehicle{ID: v, Status: vehicles.StatusRetired}, r.files, nil
}

func (r *fakeRepo) Approve(_ context.Context, record vehicles.ApproveRecord) (vehicles.Vehicle, error) {
	r.approved = append(r.approved, record)

	return vehicles.Vehicle{ID: record.VehicleID, Status: vehicles.StatusApproved, Year: record.Year, Class: record.Class}, nil
}

func (r *fakeRepo) Reject(_ context.Context, record vehicles.RejectRecord) (vehicles.Vehicle, error) {
	r.rejected = append(r.rejected, record)

	return vehicles.Vehicle{ID: record.VehicleID, Status: vehicles.StatusRejected}, nil
}

func (r *fakeRepo) ListPending(context.Context, vehicles.PendingQuery) ([]vehicles.PendingItem, error) {
	return nil, nil
}

type fakeDocuments struct{ missing []string }

func (d fakeDocuments) VehicleDocumentsMissing(context.Context, string, string) ([]string, error) {
	return d.missing, nil
}

type fakeMedia struct{ discarded []string }

func (m *fakeMedia) Discard(_ context.Context, id string) error {
	m.discarded = append(m.discarded, id)

	return nil
}

type fixedIDs struct{}

func (fixedIDs) NewID() string { return vehicleID }

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) }

func newService(repo *fakeRepo, docs fakeDocuments, media *fakeMedia) *vehicles.Service {
	return vehicles.NewService(repo, docs, media, fixedIDs{}, fixedClock{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func car() vehicles.Details {
	return vehicles.Details{Make: " Toyota ", Model: "Corolla", Color: "White", PlateNumber: " erb 12345 ", Year: 2020}
}

func stored(status vehicles.Status, year int) map[string]vehicles.Vehicle {
	return map[string]vehicles.Vehicle{vehicleID: {
		ID: vehicleID, DriverID: driverID, Make: "Toyota", Model: "Corolla", PlateNumber: "ERB 12345",
		Year: year, Class: "economy", Status: status,
	}}
}

func TestAdd(t *testing.T) {
	repo := &fakeRepo{}

	got, err := newService(repo, fakeDocuments{}, &fakeMedia{}).Add(context.Background(), driverID, car())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got.Make != "Toyota" || got.PlateNumber != "ERB 12345" || got.Class != "economy" || got.Status != vehicles.StatusPending || got.DriverID != driverID {
		t.Errorf("added %+v", got)
	}

	bad := map[string]struct {
		change func(*vehicles.Details)
		want   error
	}{
		"no year":         {func(d *vehicles.Details) { d.Year = 0 }, vehicles.ErrYearRequired},
		"too old":         {func(d *vehicles.Details) { d.Year = 1970 }, vehicles.ErrInvalidYear},
		"from the future": {func(d *vehicles.Details) { d.Year = 2030 }, vehicles.ErrInvalidYear},
		"no plate":        {func(d *vehicles.Details) { d.PlateNumber = "  " }, vehicles.ErrInvalidVehicle},
		"odd class":       {func(d *vehicles.Details) { d.Class = "limo" }, vehicles.ErrInvalidClass},
	}

	for name, c := range bad {
		in := car()
		c.change(&in)

		if _, err := newService(&fakeRepo{}, fakeDocuments{}, &fakeMedia{}).Add(context.Background(), driverID, in); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	if _, err := newService(&fakeRepo{}, fakeDocuments{}, &fakeMedia{}).Add(context.Background(), "nope", car()); !errors.Is(err, vehicles.ErrDriverNotFound) {
		t.Errorf("bad driver id: %v", err)
	}
}

func TestSomeoneElsesCarLooksLikeNone(t *testing.T) {
	repo := &fakeRepo{stored: stored(vehicles.StatusApproved, 2020)}
	svc := newService(repo, fakeDocuments{}, &fakeMedia{})

	if _, err := svc.Activate(context.Background(), otherID, vehicleID); !errors.Is(err, vehicles.ErrVehicleNotFound) {
		t.Errorf("activate: %v", err)
	}

	if _, err := svc.Retire(context.Background(), otherID, vehicleID); !errors.Is(err, vehicles.ErrVehicleNotFound) {
		t.Errorf("retire: %v", err)
	}

	if _, err := svc.Update(context.Background(), otherID, vehicleID, car()); !errors.Is(err, vehicles.ErrVehicleNotFound) {
		t.Errorf("update: %v", err)
	}

	if len(repo.activated)+len(repo.retired)+len(repo.updated) != 0 {
		t.Error("the repository was written to")
	}
}

func TestUpdate_OnlyWhileUnderReview(t *testing.T) {
	for _, status := range []vehicles.Status{vehicles.StatusApproved, vehicles.StatusRetired} {
		repo := &fakeRepo{stored: stored(status, 2020)}

		if _, err := newService(repo, fakeDocuments{}, &fakeMedia{}).Update(context.Background(), driverID, vehicleID, car()); !errors.Is(err, vehicles.ErrVehicleNotEditable) {
			t.Errorf("%s: %v", status, err)
		}
	}

	repo := &fakeRepo{stored: stored(vehicles.StatusRejected, 2020)}
	if _, err := newService(repo, fakeDocuments{}, &fakeMedia{}).Update(context.Background(), driverID, vehicleID, car()); err != nil {
		t.Fatalf("a rejected car: %v", err)
	}
}

func TestRetire_DeletesTheCarsFiles(t *testing.T) {
	repo := &fakeRepo{stored: stored(vehicles.StatusApproved, 2020), files: []string{"f1", "f2"}}
	media := &fakeMedia{}

	if _, err := newService(repo, fakeDocuments{}, media).Retire(context.Background(), driverID, vehicleID); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(media.discarded, []string{"f1", "f2"}) {
		t.Errorf("discarded %v", media.discarded)
	}
}

func TestApprove(t *testing.T) {
	repo := &fakeRepo{stored: stored(vehicles.StatusPending, 0)}
	svc := newService(repo, fakeDocuments{}, &fakeMedia{})

	if _, err := svc.Approve(context.Background(), vehicles.ApproveInput{VehicleID: vehicleID}); !errors.Is(err, vehicles.ErrYearRequired) {
		t.Fatalf("no year anywhere: %v", err)
	}

	if _, err := svc.Approve(context.Background(), vehicles.ApproveInput{VehicleID: vehicleID, Year: 2018, Class: "Comfort", ReviewedBy: staffID}); err != nil {
		t.Fatal(err)
	}

	if got := repo.approved[0]; got.Year != 2018 || got.Class != "comfort" || got.ReviewedBy != staffID {
		t.Errorf("approved with %+v", got)
	}

	missing := &fakeRepo{stored: stored(vehicles.StatusPending, 2020)}
	_, err := newService(missing, fakeDocuments{missing: []string{"vehicle_registration_front"}}, &fakeMedia{}).
		Approve(context.Background(), vehicles.ApproveInput{VehicleID: vehicleID})

	if !errors.Is(err, vehicles.ErrDocumentsIncomplete) || len(missing.approved) != 0 {
		t.Fatalf("documents missing: %v", err)
	}

	approved := &fakeRepo{stored: stored(vehicles.StatusApproved, 2020)}
	if _, err := newService(approved, fakeDocuments{}, &fakeMedia{}).Approve(context.Background(), vehicles.ApproveInput{VehicleID: vehicleID}); !errors.Is(err, vehicles.ErrVehicleNotPending) {
		t.Fatalf("approving twice: %v", err)
	}
}

func TestReject_NeedsAReason(t *testing.T) {
	repo := &fakeRepo{stored: stored(vehicles.StatusPending, 2020)}
	svc := newService(repo, fakeDocuments{}, &fakeMedia{})

	if _, err := svc.Reject(context.Background(), vehicles.RejectInput{VehicleID: vehicleID, Reason: " "}); !errors.Is(err, vehicles.ErrReasonRequired) {
		t.Fatalf("no reason: %v", err)
	}

	if _, err := svc.Reject(context.Background(), vehicles.RejectInput{VehicleID: vehicleID, Reason: " plate unreadable "}); err != nil {
		t.Fatal(err)
	}

	if repo.rejected[0].Reason != "plate unreadable" {
		t.Errorf("rejected with %+v", repo.rejected[0])
	}
}
