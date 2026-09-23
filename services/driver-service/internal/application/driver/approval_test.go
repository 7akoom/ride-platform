package driver_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// --- going online needs approval ------------------------------------------

func TestUpdateAvailability_PendingAndRejectedCannotGoOnline(t *testing.T) {
	for _, status := range []driver.Status{driver.StatusPending, driver.StatusRejected} {
		for _, target := range []driver.AvailabilityStatus{driver.AvailabilityAvailable, driver.AvailabilityBusy} {
			repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: status}}
			svc := newService(repo, "id")

			_, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
				DriverID:           "d1",
				AvailabilityStatus: target,
			})

			if !errors.Is(err, driver.ErrDriverNotApproved) {
				t.Errorf("status %q going %q: got %v, want ErrDriverNotApproved", status, target, err)
			}

			if len(repo.updateAvailabilityCalls) != 0 {
				t.Errorf("status %q going %q: the repository was written to", status, target)
			}
		}
	}
}

func TestUpdateAvailability_PendingDriverMayStayOffline(t *testing.T) {
	repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: driver.StatusPending}}
	svc := newService(repo, "id")

	_, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
		DriverID:           "d1",
		AvailabilityStatus: driver.AvailabilityOffline,
	})
	if err != nil {
		t.Fatalf("offline for a pending driver: %v", err)
	}

	if len(repo.updateAvailabilityCalls) != 1 {
		t.Fatalf("update calls = %d, want 1", len(repo.updateAvailabilityCalls))
	}
}

func TestUpdateAvailability_ActiveAndSuspendedMayChange(t *testing.T) {
	// Suspended stays allowed on purpose: the lifecycle reconciler has to be
	// able to release a suspended driver from busy after a running trip.
	for _, status := range []driver.Status{driver.StatusActive, driver.StatusSuspended} {
		repo := &fakeRepository{findByIDResult: driver.Driver{ID: "d1", Status: status}}
		svc := newService(repo, "id")

		_, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
			DriverID:           "d1",
			AvailabilityStatus: driver.AvailabilityAvailable,
		})
		if err != nil {
			t.Errorf("status %q going available: %v", status, err)
		}
	}
}

func TestUpdateAvailability_UnknownDriverGoingOnlineIsNotFound(t *testing.T) {
	repo := &fakeRepository{findByIDErr: driver.ErrDriverNotFound}
	svc := newService(repo, "id")

	_, err := svc.UpdateAvailability(context.Background(), driver.UpdateDriverAvailabilityInput{
		DriverID:           "d1",
		AvailabilityStatus: driver.AvailabilityAvailable,
	})
	if !errors.Is(err, driver.ErrDriverNotFound) {
		t.Fatalf("got %v, want ErrDriverNotFound", err)
	}
}

// --- approve and reject ------------------------------------------------------

func TestApproveDriver(t *testing.T) {
	repo := &fakeRepository{updateStatusResult: driver.Driver{ID: "d1", Status: driver.StatusActive}}
	svc := newService(repo, "id")

	got, err := svc.ApproveDriver(context.Background(), "  d1  ")
	if err != nil {
		t.Fatalf("ApproveDriver: %v", err)
	}

	if got.Status != driver.StatusActive {
		t.Errorf("status = %q, want active", got.Status)
	}

	if len(repo.updateStatusCalls) != 1 {
		t.Fatalf("update calls = %d, want 1", len(repo.updateStatusCalls))
	}

	call := repo.updateStatusCalls[0]

	if call.DriverID != "d1" {
		t.Errorf("driver id = %q, want the trimmed id", call.DriverID)
	}

	if call.To != driver.StatusActive {
		t.Errorf("to = %q, want active", call.To)
	}

	want := []driver.Status{driver.StatusPending, driver.StatusRejected}
	if !slices.Equal(call.AllowedFrom, want) {
		t.Errorf("allowed from = %v, want %v", call.AllowedFrom, want)
	}
}

func TestRejectDriver(t *testing.T) {
	repo := &fakeRepository{updateStatusResult: driver.Driver{ID: "d1", Status: driver.StatusRejected}}
	svc := newService(repo, "id")

	got, err := svc.RejectDriver(context.Background(), "d1", "")
	if err != nil {
		t.Fatalf("RejectDriver: %v", err)
	}

	if got.Status != driver.StatusRejected {
		t.Errorf("status = %q, want rejected", got.Status)
	}

	call := repo.updateStatusCalls[0]

	if call.To != driver.StatusRejected {
		t.Errorf("to = %q, want rejected", call.To)
	}

	// An approved driver is never "rejected": that would be a suspension.
	if !slices.Equal(call.AllowedFrom, []driver.Status{driver.StatusPending}) {
		t.Errorf("allowed from = %v, want only pending", call.AllowedFrom)
	}
}

func TestApproveAndReject_RequireDriverID(t *testing.T) {
	repo := &fakeRepository{}
	svc := newService(repo, "id")

	if _, err := svc.ApproveDriver(context.Background(), "   "); !errors.Is(err, driver.ErrDriverIDRequired) {
		t.Errorf("ApproveDriver: got %v, want ErrDriverIDRequired", err)
	}

	if _, err := svc.RejectDriver(context.Background(), "", ""); !errors.Is(err, driver.ErrDriverIDRequired) {
		t.Errorf("RejectDriver: got %v, want ErrDriverIDRequired", err)
	}

	if len(repo.updateStatusCalls) != 0 {
		t.Error("the repository was called with a blank id")
	}
}

func TestApproveAndReject_PassRepositoryErrorsThrough(t *testing.T) {
	for _, repoErr := range []error{driver.ErrDriverNotFound, driver.ErrInvalidStatusTransition} {
		repo := &fakeRepository{updateStatusErr: repoErr}
		svc := newService(repo, "id")

		if _, err := svc.ApproveDriver(context.Background(), "d1"); !errors.Is(err, repoErr) {
			t.Errorf("ApproveDriver: got %v, want %v", err, repoErr)
		}

		if _, err := svc.RejectDriver(context.Background(), "d1", ""); !errors.Is(err, repoErr) {
			t.Errorf("RejectDriver: got %v, want %v", err, repoErr)
		}
	}
}

func TestRejectDriver_KeepsTheReasonAndApprovalClearsIt(t *testing.T) {
	repo := &fakeRepository{updateStatusResult: driver.Driver{ID: "d1"}}
	svc := newService(repo, "id")

	if _, err := svc.RejectDriver(context.Background(), "d1", "  licence photo is blurry  "); err != nil {
		t.Fatalf("RejectDriver: %v", err)
	}

	if _, err := svc.ApproveDriver(context.Background(), "d1"); err != nil {
		t.Fatalf("ApproveDriver: %v", err)
	}

	if repo.updateStatusCalls[0].Reason != "licence photo is blurry" {
		t.Errorf("reject reason = %q", repo.updateStatusCalls[0].Reason)
	}

	if repo.updateStatusCalls[1].Reason != "" {
		t.Errorf("approve must clear the reason, got %q", repo.updateStatusCalls[1].Reason)
	}

	long := make([]rune, 501)
	for i := range long {
		long[i] = 'x'
	}

	if _, err := svc.RejectDriver(context.Background(), "d1", string(long)); !errors.Is(err, driver.ErrRejectionReasonTooLong) {
		t.Errorf("a 501-character reason: got %v", err)
	}
}

func TestListDrivers_PagesWithAnOpaqueToken(t *testing.T) {
	ids := []string{
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000001",
	}

	repo := &fakeRepository{}
	for _, id := range ids {
		repo.listResult = append(repo.listResult, driver.Driver{ID: id})
	}

	svc := newService(repo, "id")

	page, err := svc.ListDrivers(context.Background(), driver.ListDriversQuery{Status: driver.StatusPending, PageSize: 2})
	if err != nil {
		t.Fatalf("ListDrivers: %v", err)
	}

	if len(page.Drivers) != 2 || page.NextPageToken == "" {
		t.Fatalf("page = %d drivers, token %q", len(page.Drivers), page.NextPageToken)
	}

	if repo.listCalls[0].Limit != 3 || repo.listCalls[0].Status != driver.StatusPending || repo.listCalls[0].AfterID != "" {
		t.Fatalf("first query = %+v", repo.listCalls[0])
	}

	repo.listResult = repo.listResult[2:]

	next, err := svc.ListDrivers(context.Background(), driver.ListDriversQuery{PageSize: 2, PageToken: page.NextPageToken})
	if err != nil || len(next.Drivers) != 1 || next.NextPageToken != "" {
		t.Fatalf("second page: %+v, %v", next, err)
	}

	if repo.listCalls[1].AfterID != ids[1] {
		t.Fatalf("the cursor must be the last driver of the previous page, got %q", repo.listCalls[1].AfterID)
	}

	for _, bad := range []driver.ListDriversQuery{
		{PageToken: "not-a-token"},
		{PageSize: -1},
		{Status: "flying"},
	} {
		if _, err := svc.ListDrivers(context.Background(), bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
