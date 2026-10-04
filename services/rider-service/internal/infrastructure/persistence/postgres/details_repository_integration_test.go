package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/profile"
)

func TestDetailsRepository(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDetailsRepository(pool)
	riderID, _ := addRider(t, pool)

	empty, err := repo.GetDetails(ctx, riderID)
	if err != nil || empty.Fields != (profile.Fields{}) || empty.PhotoMediaID != "" {
		t.Fatalf("empty: %+v %v", empty, err)
	}

	// A photo first, then the fields: neither overwrites the other.
	first := uuid.NewString()

	withPhoto, old, err := repo.SetPhoto(ctx, riderID, first, time.Now())
	if err != nil || old != "" || withPhoto.PhotoMediaID != first {
		t.Fatalf("set photo: %+v %q %v", withPhoto, old, err)
	}

	fields := profile.Fields{Gender: "female", DateOfBirth: "1999-12-31", Nationality: "TR"}

	saved, err := repo.SaveFields(ctx, riderID, fields, time.Now())
	if err != nil || saved.Fields != fields || saved.PhotoMediaID != first {
		t.Fatalf("save fields: %+v %v", saved, err)
	}

	second := uuid.NewString()

	replaced, old, err := repo.SetPhoto(ctx, riderID, second, time.Now())
	if err != nil || old != first || replaced.PhotoMediaID != second || replaced.Fields != fields {
		t.Fatalf("replace photo: %+v %q %v", replaced, old, err)
	}

	removed, old, err := repo.SetPhoto(ctx, riderID, "", time.Now())
	if err != nil || old != second || removed.PhotoMediaID != "" {
		t.Fatalf("remove photo: %+v %q %v", removed, old, err)
	}

	got, err := repo.GetDetails(ctx, riderID)
	if err != nil || got.Fields != fields || got.PhotoMediaID != "" {
		t.Fatalf("read back: %+v %v", got, err)
	}
}
