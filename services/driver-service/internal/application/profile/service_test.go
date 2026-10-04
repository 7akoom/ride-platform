package profile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

type fakeRepo struct {
	details Details
	changes []NameChange
}

func (r *fakeRepo) GetDetails(_ context.Context, id string) (Details, error) {
	d := r.details
	d.DriverID = id

	return d, nil
}

func (r *fakeRepo) SaveFields(_ context.Context, id string, f Fields, at time.Time) (Details, error) {
	r.details.Fields, r.details.UpdatedAt = f, at

	return r.GetDetails(context.Background(), id)
}

func (r *fakeRepo) ApprovedPhoto(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (r *fakeRepo) CreateNameChange(_ context.Context, c NameChange) (NameChange, error) {
	for _, existing := range r.changes {
		if existing.Status == NameChangePending {
			return NameChange{}, ErrNameChangeOpen
		}
	}

	r.changes = append(r.changes, c)

	return c, nil
}

func (r *fakeRepo) ListNameChanges(context.Context, string) ([]NameChange, error) {
	return r.changes, nil
}

func (r *fakeRepo) ListPendingNameChanges(context.Context, int) ([]NameChange, error) {
	return r.changes, nil
}

func (r *fakeRepo) DecideNameChange(_ context.Context, id string, decide func(*NameChange) error) (NameChange, error) {
	for i := range r.changes {
		if r.changes[i].ID == id {
			c := r.changes[i]
			if err := decide(&c); err != nil {
				return NameChange{}, err
			}

			r.changes[i] = c

			return c, nil
		}
	}

	return NameChange{}, ErrNameChangeNotFound
}

type fakeDrivers struct{ d driver.Driver }

func (f fakeDrivers) GetDriver(context.Context, string) (driver.Driver, error) { return f.d, nil }

type noMedia struct{}

func (noMedia) DownloadURL(context.Context, string) (string, time.Time, error) {
	return "", time.Time{}, errors.New("unused")
}

type seqIDs struct{ n int }

func (s *seqIDs) NewID() string { s.n++; return string(rune('a' + s.n)) }

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC) }

func newTestService(status driver.Status, details Fields) (*Service, *fakeRepo) {
	repo := &fakeRepo{details: Details{Fields: details}}
	d := driver.Driver{ID: "d1", DisplayName: "Ali Hasan", Status: status}

	return NewService(repo, fakeDrivers{d}, noMedia{}, &seqIDs{}, fixedClock{}), repo
}

func TestUpdateLockedOnceApproved(t *testing.T) {
	ctx := context.Background()

	svc, _ := newTestService(driver.StatusActive, Fields{Gender: "male"})

	if _, err := svc.Update(ctx, "d1", Patch{Gender: ptr("female")}); !errors.Is(err, ErrDetailsLocked) {
		t.Fatalf("change given gender: %v", err)
	}

	if _, err := svc.Update(ctx, "d1", Patch{Gender: ptr("")}); !errors.Is(err, ErrDetailsLocked) {
		t.Fatalf("clear given gender: %v", err)
	}

	got, err := svc.Update(ctx, "d1", Patch{Nationality: ptr("iq"), DateOfBirth: ptr("1990-01-01")})
	if err != nil || got.Fields.Nationality != "IQ" {
		t.Fatalf("fill empty fields: %+v %v", got, err)
	}

	pending, _ := newTestService(driver.StatusPending, Fields{Gender: "male"})
	if _, err := pending.Update(ctx, "d1", Patch{Gender: ptr("female")}); err != nil {
		t.Fatalf("before approval anything changes: %v", err)
	}
}

func TestNameChangeRules(t *testing.T) {
	ctx := context.Background()

	notYet, _ := newTestService(driver.StatusPending, Fields{})
	if _, err := notYet.RequestNameChange(ctx, "d1", "Other", ""); !errors.Is(err, ErrNameChangeNotNeeded) {
		t.Fatalf("not approved: %v", err)
	}

	svc, _ := newTestService(driver.StatusActive, Fields{})

	if _, err := svc.RequestNameChange(ctx, "d1", "  Ali   Hasan ", ""); !errors.Is(err, ErrNameUnchanged) {
		t.Fatalf("same name: %v", err)
	}

	if _, err := svc.RequestNameChange(ctx, "d1", "   ", ""); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("empty name: %v", err)
	}

	c, err := svc.RequestNameChange(ctx, "d1", " Ali  Hassan ", "spelling")
	if err != nil || c.RequestedName != "Ali Hassan" || c.CurrentName != "Ali Hasan" || c.Status != NameChangePending {
		t.Fatalf("request: %+v %v", c, err)
	}

	if _, err := svc.RejectNameChange(ctx, c.ID, "staff", " "); !errors.Is(err, ErrRejectionRequired) {
		t.Fatalf("reject without reason: %v", err)
	}

	approved, err := svc.ApproveNameChange(ctx, c.ID, "staff")
	if err != nil || approved.Status != NameChangeApproved || approved.DecidedAt == nil {
		t.Fatalf("approve: %+v %v", approved, err)
	}

	if _, err := svc.ApproveNameChange(ctx, c.ID, "staff"); !errors.Is(err, ErrNameChangeNotPending) {
		t.Fatalf("approve twice: %v", err)
	}
}
