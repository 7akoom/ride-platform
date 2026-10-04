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

func TestStoreFileKeepsADataExportForItsOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	zip := []byte("PK\x03\x04 a zip")

	stored, err := f.service.StoreFile(ctx, ownerA, PurposeDataExport, TypeZIP, zip)
	if err != nil {
		t.Fatal(err)
	}

	if stored.Status != StatusReady || stored.OwnerIdentityID != ownerA || stored.SizeBytes != int64(len(zip)) || !stored.UploadCleared {
		t.Fatalf("stored %+v", stored)
	}

	if obj, ok := f.store.object(stored.ObjectKey); !ok || string(obj.data) != string(zip) {
		t.Fatal("the bytes are not in the store")
	}

	if _, _, err := f.service.DownloadURL(ctx, stored.ID); err != nil {
		t.Fatalf("download: %v", err)
	}

	for name, call := range map[string]func() error{
		"another purpose": func() error {
			_, err := f.service.StoreFile(ctx, ownerA, PurposeProfilePhoto, TypeZIP, zip)
			return err
		},
		"another type": func() error { _, err := f.service.StoreFile(ctx, ownerA, PurposeDataExport, TypePDF, zip); return err },
		"empty":        func() error { _, err := f.service.StoreFile(ctx, ownerA, PurposeDataExport, TypeZIP, nil); return err },
		"no owner":     func() error { _, err := f.service.StoreFile(ctx, "x", PurposeDataExport, TypeZIP, zip); return err },
	} {
		if call() == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if _, _, err := f.service.CreateUpload(ctx, CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeDataExport, ContentType: TypeZIP, SizeBytes: 10}); !errors.Is(err, ErrInvalidPurpose) {
		t.Fatalf("a data export upload: %v", err)
	}
}
