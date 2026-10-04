package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/profile"
)

func TestProfileRepositoryDetailsAndPhoto(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewProfileRepository(pool)
	driverID := addDriver(t, pool, "offline")

	empty, err := repo.GetDetails(ctx, driverID)
	if err != nil || empty.Fields != (profile.Fields{}) || empty.HasPhoto {
		t.Fatalf("empty details: %+v %v", empty, err)
	}

	fields := profile.Fields{Gender: "male", DateOfBirth: "1990-05-17", Nationality: "IQ"}

	saved, err := repo.SaveFields(ctx, driverID, fields, time.Now())
	if err != nil || saved.Fields != fields {
		t.Fatalf("save: %+v %v", saved, err)
	}

	again, err := repo.GetDetails(ctx, driverID)
	if err != nil || again.Fields != fields {
		t.Fatalf("read back: %+v %v", again, err)
	}

	cleared, err := repo.SaveFields(ctx, driverID, profile.Fields{Gender: "male"}, time.Now())
	if err != nil || cleared.Fields.DateOfBirth != "" || cleared.Fields.Nationality != "" {
		t.Fatalf("clear: %+v %v", cleared, err)
	}

	if _, ok, err := repo.ApprovedPhoto(ctx, driverID); err != nil || ok {
		t.Fatalf("no photo yet: %v %v", ok, err)
	}

	pending, approved := uuid.NewString(), uuid.NewString()

	for _, row := range []struct{ media, status string }{{pending, "pending"}, {approved, "approved"}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO driver_documents (id, driver_id, type_code, media_id, status, reviewed_at)
			 VALUES ($1, $2, 'profile_photo', $3, $4, now())`,
			uuid.NewString(), driverID, row.media, row.status); err != nil {
			t.Fatal(err)
		}
	}

	mediaID, ok, err := repo.ApprovedPhoto(ctx, driverID)
	if err != nil || !ok || mediaID != approved {
		t.Fatalf("approved photo: %q %v %v", mediaID, ok, err)
	}

	if d, err := repo.GetDetails(ctx, driverID); err != nil || !d.HasPhoto {
		t.Fatalf("has photo: %+v %v", d, err)
	}
}

func TestProfileRepositoryNameChanges(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewProfileRepository(pool)
	driverID := addDriver(t, pool, "offline")

	request := func(name string) (profile.NameChange, error) {
		return repo.CreateNameChange(ctx, profile.NameChange{
			ID: uuid.NewString(), DriverID: driverID, CurrentName: "Test Driver",
			RequestedName: name, Reason: "typo", Status: profile.NameChangePending, CreatedAt: time.Now(),
		})
	}

	first, err := request("Ali Hasan")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := request("Another Name"); !errors.Is(err, profile.ErrNameChangeOpen) {
		t.Fatalf("second pending: want ErrNameChangeOpen, got %v", err)
	}

	pending, err := repo.ListPendingNameChanges(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending: %+v %v", pending, err)
	}

	reviewer := uuid.NewString()

	decided, err := repo.DecideNameChange(ctx, first.ID, func(c *profile.NameChange) error {
		now := time.Now()
		c.Status, c.DecidedBy, c.DecidedAt = profile.NameChangeApproved, reviewer, &now

		return nil
	})
	if err != nil || decided.Status != profile.NameChangeApproved {
		t.Fatalf("approve: %+v %v", decided, err)
	}

	var name string
	if err := pool.QueryRow(ctx, `SELECT display_name FROM drivers WHERE id = $1`, driverID).Scan(&name); err != nil || name != "Ali Hasan" {
		t.Fatalf("driver renamed: %q %v", name, err)
	}

	got := events(t, pool, driverID, NameChangeReviewedEvent)
	if len(got) != 1 || !strings.Contains(got[0], `"decision": "approved"`) && !strings.Contains(got[0], `"decision":"approved"`) {
		t.Fatalf("event: %v", got)
	}

	// The decision is final.
	if _, err := repo.DecideNameChange(ctx, first.ID, func(c *profile.NameChange) error {
		if c.Status != profile.NameChangePending {
			return profile.ErrNameChangeNotPending
		}

		return nil
	}); !errors.Is(err, profile.ErrNameChangeNotPending) {
		t.Fatalf("decide twice: %v", err)
	}

	// A new request is possible once the previous one is decided; rejecting keeps the name.
	second, err := request("Someone Else")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.DecideNameChange(ctx, second.ID, func(c *profile.NameChange) error {
		now := time.Now()
		c.Status, c.RejectionReason, c.DecidedBy, c.DecidedAt = profile.NameChangeRejected, "does not match ID", "", &now

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := pool.QueryRow(ctx, `SELECT display_name FROM drivers WHERE id = $1`, driverID).Scan(&name); err != nil || name != "Ali Hasan" {
		t.Fatalf("rejected keeps name: %q %v", name, err)
	}

	history, err := repo.ListNameChanges(ctx, driverID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}

	if _, err := repo.DecideNameChange(ctx, "not-a-uuid", func(*profile.NameChange) error { return nil }); !errors.Is(err, profile.ErrNameChangeNotFound) {
		t.Fatalf("bad id: %v", err)
	}
}
