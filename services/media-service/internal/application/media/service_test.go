package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateUploadValidates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		input CreateUploadInput
		want  error
	}{
		{"no owner", CreateUploadInput{Purpose: PurposeProfilePhoto, ContentType: TypeJPEG, SizeBytes: 10}, ErrOwnerRequired},
		{"a service as owner", CreateUploadInput{OwnerIdentityID: "internal-service", Purpose: PurposeProfilePhoto, ContentType: TypeJPEG, SizeBytes: 10}, ErrOwnerRequired},
		{"unknown purpose", CreateUploadInput{OwnerIdentityID: ownerA, Purpose: "selfie", ContentType: TypeJPEG, SizeBytes: 10}, ErrInvalidPurpose},
		{"PDF as a profile photo", CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeProfilePhoto, ContentType: TypePDF, SizeBytes: 10}, ErrTypeNotAllowed},
		{"HTML", CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeDriverDocument, ContentType: "text/html", SizeBytes: 10}, ErrTypeNotAllowed},
		{"SVG", CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeDriverDocument, ContentType: "image/svg+xml", SizeBytes: 10}, ErrTypeNotAllowed},
		{"zero bytes", CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeProfilePhoto, ContentType: TypeJPEG}, ErrInvalidSize},
		{"over the limit", CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeProfilePhoto, ContentType: TypeJPEG, SizeBytes: 5*megabyte + 1}, ErrInvalidSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := f.service.CreateUpload(ctx, tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	if len(f.repo.records) != 0 {
		t.Fatal("a refused upload must not leave a record")
	}
}

func TestCreateUploadSignsTheIncomingKeyForTheDeclaredFile(t *testing.T) {
	f := newFixture(t)

	created, ticket, err := f.service.CreateUpload(context.Background(), CreateUploadInput{
		OwnerIdentityID: ownerA,
		Purpose:         PurposeDriverDocument,
		ContentType:     " Image/JPEG ",
		SizeBytes:       1234,
	})
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}

	if created.Status != StatusPending || created.DeclaredContentType != TypeJPEG || created.DeclaredSize != 1234 {
		t.Fatalf("got %+v", created)
	}

	if created.ObjectKey != "driver_document/2026/09/"+created.ID {
		t.Fatalf("object key %q", created.ObjectKey)
	}

	if ticket.Method != "PUT" || ticket.Headers["Content-Type"] != TypeJPEG || ticket.Headers["Content-Length"] != "1234" {
		t.Fatalf("ticket %+v", ticket)
	}

	if f.store.presigned[0] != "PUT incoming/"+created.ObjectKey {
		t.Fatalf("signed %q; uploads must go to the incoming key", f.store.presigned[0])
	}

	if !ticket.ExpiresAt.Equal(testNow.Add(testSettings.UploadURLTTL)) {
		t.Fatalf("expires %v", ticket.ExpiresAt)
	}
}

func TestCreateUploadLimitsOpenReservations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	input := CreateUploadInput{OwnerIdentityID: ownerA, Purpose: PurposeProfilePhoto, ContentType: TypePNG, SizeBytes: 10}

	for i := 0; i < testSettings.MaxPendingPerOwner; i++ {
		if _, _, err := f.service.CreateUpload(ctx, input); err != nil {
			t.Fatalf("upload %d: %v", i, err)
		}
	}

	if _, _, err := f.service.CreateUpload(ctx, input); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("got %v, want ErrTooManyPending", err)
	}

	input.OwnerIdentityID = ownerB
	if _, _, err := f.service.CreateUpload(ctx, input); err != nil {
		t.Fatalf("another owner has their own limit: %v", err)
	}
}

func TestCompleteUploadStoresOnlyTheCheckedBytes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	original := withExif(t, jpegSample(t, 80, 40), 1, "GPSLatitude")
	created := f.upload(t, PurposeDriverDocument, TypeJPEG, original)

	f.clock.advance(time.Minute)

	ready, err := f.service.CompleteUpload(ctx, created.ID)
	if err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}

	if ready.Status != StatusReady || ready.ContentType != TypeJPEG || ready.Width != 80 || ready.Height != 40 {
		t.Fatalf("got %+v", ready)
	}

	if ready.CompletedAt == nil || !ready.CompletedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("completed at %v", ready.CompletedAt)
	}

	stored, ok := f.store.object(created.ObjectKey)
	if !ok {
		t.Fatal("nothing stored at the object key")
	}

	if bytes.Equal(stored.data, original) || bytes.Contains(stored.data, []byte("GPSLatitude")) {
		t.Fatal("the stored bytes are the upload, not the re-encoded image")
	}

	if int64(len(stored.data)) != ready.SizeBytes || hexSHA256(stored.data) != ready.SHA256 {
		t.Fatal("size and sha256 must describe the stored bytes")
	}

	if _, still := f.store.object(created.UploadKey()); still {
		t.Fatal("the incoming object must be deleted after completion")
	}

	again, err := f.service.CompleteUpload(ctx, created.ID)
	if err != nil || again.SHA256 != ready.SHA256 {
		t.Fatalf("completing again: %+v %v", again, err)
	}
}

func TestCompleteUploadKeepsPDFBytes(t *testing.T) {
	f := newFixture(t)
	document := pdfSample("/Pages 2 0 R")
	created := f.upload(t, PurposeDriverDocument, TypePDF, document)

	ready, err := f.service.CompleteUpload(context.Background(), created.ID)
	if err != nil || ready.Status != StatusReady || ready.ContentType != TypePDF {
		t.Fatalf("got %+v %v", ready, err)
	}

	stored, _ := f.store.object(created.ObjectKey)
	if !bytes.Equal(stored.data, document) {
		t.Fatal("a PDF is stored as uploaded")
	}
}

func TestAPutAfterCompletionCannotReplaceTheCheckedFile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	image := pngSample(t, 20, 20)
	created := f.upload(t, PurposeProfilePhoto, TypePNG, image)

	if _, err := f.service.CompleteUpload(ctx, created.ID); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}

	before, _ := f.store.object(created.ObjectKey)

	// The upload URL is still valid: the client sends something else, of the
	// same declared type and size, and completes again.
	evil := append([]byte("<html><script>steal()</script>"), bytes.Repeat([]byte(" "), len(image)-30)...)
	f.store.put(created.UploadKey(), TypePNG, evil)

	if _, err := f.service.CompleteUpload(ctx, created.ID); err != nil {
		t.Fatalf("CompleteUpload again: %v", err)
	}

	after, _ := f.store.object(created.ObjectKey)
	if !bytes.Equal(before.data, after.data) {
		t.Fatal("the checked file was replaced")
	}

	url, _, err := f.service.DownloadURL(ctx, created.ID)
	if err != nil || !strings.HasSuffix(url, "/"+created.ObjectKey) || strings.Contains(url, "incoming/") {
		t.Fatalf("download URL %q %v must read the checked object", url, err)
	}
}

func TestCompleteUploadRejects(t *testing.T) {
	cases := []struct {
		name   string
		upload func(f fixture) Media
		want   string
	}{
		{
			name: "wrong content",
			upload: func(f fixture) Media {
				return f.upload(t, PurposeProfilePhoto, TypeJPEG, pngSample(t, 8, 8))
			},
			want: rejectWrongType,
		},
		{
			name: "a size other than declared",
			upload: func(f fixture) Media {
				created, _, _ := f.service.CreateUpload(context.Background(), CreateUploadInput{
					OwnerIdentityID: ownerA, Purpose: PurposeProfilePhoto, ContentType: TypePNG, SizeBytes: 50,
				})
				f.store.put(created.UploadKey(), TypePNG, pngSample(t, 8, 8))

				return created
			},
			want: "the file size does not match the declared size",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			created := tc.upload(f)

			rejected, err := f.service.CompleteUpload(ctx, created.ID)
			if err != nil {
				t.Fatalf("CompleteUpload: %v", err)
			}

			if rejected.Status != StatusRejected || rejected.RejectionReason != tc.want {
				t.Fatalf("got %s %q", rejected.Status, rejected.RejectionReason)
			}

			if _, still := f.store.object(created.UploadKey()); still {
				t.Fatal("a rejected upload must be deleted")
			}

			if _, stored := f.store.object(created.ObjectKey); stored {
				t.Fatal("nothing may be stored for a rejected upload")
			}

			if _, err := f.service.CompleteUpload(ctx, created.ID); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("completing a rejected upload: %v", err)
			}

			if _, _, err := f.service.DownloadURL(ctx, created.ID); !errors.Is(err, ErrNotReady) {
				t.Fatalf("download of a rejected upload: %v", err)
			}
		})
	}
}

func TestCompleteUploadBeforeTheBytesArrive(t *testing.T) {
	f := newFixture(t)

	created, _, err := f.service.CreateUpload(context.Background(), CreateUploadInput{
		OwnerIdentityID: ownerA, Purpose: PurposeProfilePhoto, ContentType: TypePNG, SizeBytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.CompleteUpload(context.Background(), created.ID); !errors.Is(err, ErrNotUploaded) {
		t.Fatalf("got %v, want ErrNotUploaded", err)
	}

	found, _ := f.service.Get(context.Background(), created.ID)
	if found.Status != StatusPending {
		t.Fatal("the reservation stays open until the bytes arrive")
	}
}

func TestCompleteUploadFailsWithoutMarkingWhenTheStoreFails(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 8, 8))
	f.store.failPut = errors.New("disk full")

	if _, err := f.service.CompleteUpload(context.Background(), created.ID); err == nil {
		t.Fatal("expected an error")
	}

	found, _ := f.service.Get(context.Background(), created.ID)
	if found.Status != StatusPending {
		t.Fatalf("status %s; a file that could not be stored must not be READY", found.Status)
	}
}

func TestConcurrentCompletionsAgree(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 64, 64))

	var wg sync.WaitGroup

	results := make([]Media, 4)
	errs := make([]error, 4)

	for i := range results {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			results[i], errs[i] = f.service.CompleteUpload(context.Background(), created.ID)
		}(i)
	}

	wg.Wait()

	for i := range results {
		// A call that arrived after the incoming object was deleted cannot
		// read it; every call that returns a file returns the READY one.
		if errs[i] != nil {
			if !errors.Is(errs[i], ErrNotUploaded) {
				t.Fatalf("call %d: %v", i, errs[i])
			}

			continue
		}

		if results[i].Status != StatusReady {
			t.Fatalf("call %d: status %s", i, results[i].Status)
		}
	}
}

func TestGetValidatesTheID(t *testing.T) {
	f := newFixture(t)

	for _, id := range []string{"", "x", "../../etc/passwd", "00000000-0000-4000-8000-00000000000Z"} {
		if _, err := f.service.Get(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("id %q: got %v", id, err)
		}
	}

	if _, err := f.service.Get(context.Background(), "00000000-0000-4000-8000-000000000999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestHoldAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created := f.upload(t, PurposeDriverDocument, TypePNG, pngSample(t, 8, 8))

	if _, err := f.service.Hold(ctx, created.ID, ownerA, PurposeDriverDocument); !errors.Is(err, ErrNotReady) {
		t.Fatalf("holding a pending file: %v", err)
	}

	if _, err := f.service.CompleteUpload(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.Hold(ctx, created.ID, ownerB, PurposeDriverDocument); !errors.Is(err, ErrHoldMismatch) {
		t.Fatalf("holding for another owner: %v", err)
	}

	if _, err := f.service.Hold(ctx, created.ID, ownerA, PurposeProfilePhoto); !errors.Is(err, ErrHoldMismatch) {
		t.Fatalf("holding for another purpose: %v", err)
	}

	held, err := f.service.Hold(ctx, created.ID, strings.ToUpper(ownerA), PurposeDriverDocument)
	if err != nil || !held.Held {
		t.Fatalf("hold: %+v %v", held, err)
	}

	if again, err := f.service.Hold(ctx, created.ID, ownerA, PurposeDriverDocument); err != nil || !again.Held {
		t.Fatalf("holding twice is fine: %v", err)
	}

	if err := f.service.Delete(ctx, created.ID); !errors.Is(err, ErrHeld) {
		t.Fatalf("deleting a held file: %v", err)
	}

	if _, stored := f.store.object(created.ObjectKey); !stored {
		t.Fatal("a held file must stay")
	}

	if released, err := f.service.Release(ctx, created.ID); err != nil || released.Held {
		t.Fatalf("release: %+v %v", released, err)
	}

	if err := f.service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, stored := f.store.object(created.ObjectKey); stored {
		t.Fatal("the object must be deleted")
	}

	found, _ := f.service.Get(ctx, created.ID)
	if found.Status != StatusDeleted {
		t.Fatalf("status %s", found.Status)
	}

	if err := f.service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("deleting again is fine: %v", err)
	}

	if _, _, err := f.service.DownloadURL(ctx, created.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("download of a deleted file: %v", err)
	}
}

func TestDeletingAPendingUploadRemovesTheIncomingObject(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 8, 8))

	if err := f.service.Delete(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}

	if _, still := f.store.object(created.UploadKey()); still {
		t.Fatal("the incoming object must be deleted")
	}
}

func TestExpireStaleDropsOldReservations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	old := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 8, 8))

	f.clock.advance(20 * time.Minute)

	recent := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 8, 8))

	f.clock.advance(11 * time.Minute)

	count, err := f.service.ExpireStale(ctx, 100)
	if err != nil || count != 1 {
		t.Fatalf("expired %d, %v", count, err)
	}

	found, _ := f.service.Get(ctx, old.ID)
	if found.Status != StatusExpired || !found.UploadCleared {
		t.Fatalf("old: %+v", found)
	}

	if _, still := f.store.object(old.UploadKey()); still {
		t.Fatal("the bytes of an expired reservation must be deleted")
	}

	if found, _ := f.service.Get(ctx, recent.ID); found.Status != StatusPending {
		t.Fatal("a recent reservation must stay")
	}

	if _, err := f.service.CompleteUpload(ctx, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("completing an expired reservation: %v", err)
	}
}

func TestClearUploadsWaitsForTheUploadURLToExpire(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 8, 8))

	if _, err := f.service.CompleteUpload(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	// Sent again with the still-valid URL: an orphan the sweep must remove.
	f.store.put(created.UploadKey(), TypePNG, []byte("orphan"))

	f.clock.advance(testSettings.UploadURLTTL)

	if count, err := f.service.ClearUploads(ctx, 100); err != nil || count != 0 {
		t.Fatalf("cleared %d, %v before the URL (plus skew) expired", count, err)
	}

	f.clock.advance(clockSkew + time.Second)

	if count, err := f.service.ClearUploads(ctx, 100); err != nil || count != 1 {
		t.Fatalf("cleared %d, %v", count, err)
	}

	if _, still := f.store.object(created.UploadKey()); still {
		t.Fatal("the orphan was not removed")
	}

	if _, stored := f.store.object(created.ObjectKey); !stored {
		t.Fatal("the checked file must stay")
	}

	if count, _ := f.service.ClearUploads(ctx, 100); count != 0 {
		t.Fatal("a cleared record is not cleared again")
	}
}

func TestInspectionsAreLimited(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, PurposeProfilePhoto, TypePNG, pngSample(t, 8, 8))

	// Fill every inspection slot; a completion must wait, and give up when
	// its caller does.
	for i := 0; i < testSettings.MaxConcurrentInspections; i++ {
		f.service.inspections <- struct{}{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := f.service.CompleteUpload(ctx, created.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
}

func TestNewServiceRefusesBadSettings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	bad := testSettings
	bad.PendingTTL = bad.UploadURLTTL

	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()

	NewService(newMemoryRepository(), newMemoryStore(), &sequenceIDs{}, &manualClock{now: testNow}, bad, logger)
}
