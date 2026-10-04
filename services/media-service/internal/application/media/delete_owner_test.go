package media

import (
	"context"
	"errors"
	"testing"
)

func TestDeleteOwnerDeletesHeldAndPendingFilesOfThatOwnerOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	held := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 40, 30))
	if _, err := f.service.CompleteUpload(ctx, held.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.Hold(ctx, held.ID, ownerA, PurposeProfilePhoto); err != nil {
		t.Fatal(err)
	}

	pending := f.upload(t, PurposeAddressPhoto, TypePNG, pngSample(t, 20, 20))

	others, _, err := f.service.CreateUpload(ctx, CreateUploadInput{
		OwnerIdentityID: ownerB, Purpose: PurposeProfilePhoto, ContentType: TypePNG, SizeBytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := f.service.DeleteOwner(ctx, ownerA)
	if err != nil || deleted != 2 {
		t.Fatalf("deleted %d, %v", deleted, err)
	}

	for _, id := range []string{held.ID, pending.ID} {
		if got, _ := f.repo.FindByID(ctx, id); got.Status != StatusDeleted || got.Held {
			t.Fatalf("%s: %+v", id, got)
		}
	}

	if got, _ := f.repo.FindByID(ctx, others.ID); got.Status != StatusPending {
		t.Fatalf("another owner's file was touched: %+v", got)
	}

	if again, err := f.service.DeleteOwner(ctx, ownerA); err != nil || again != 0 {
		t.Fatalf("second call: %d %v", again, err)
	}

	if _, err := f.service.DeleteOwner(ctx, "nobody"); !errors.Is(err, ErrOwnerRequired) {
		t.Fatalf("bad owner: %v", err)
	}
}
