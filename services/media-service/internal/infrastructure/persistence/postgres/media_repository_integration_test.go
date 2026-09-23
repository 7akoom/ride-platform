package postgres

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	MEDIA_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply the migration's Up section themselves (psql must be on PATH) and
// drop everything afterwards. Without the variable they are skipped.

func testRepository(t *testing.T) (*MediaRepository, *pgxpool.Pool) {
	t.Helper()

	url := os.Getenv("MEDIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MEDIA_TEST_DATABASE_URL is not set")
	}

	migration, err := os.ReadFile("../../../../migrations/00001_create_media_objects.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	up, down, ok := strings.Cut(string(migration), "-- +goose Down")
	if !ok {
		t.Fatal("migration has no Down section")
	}

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	psql(down)
	psql(up)
	t.Cleanup(func() { psql(down) })

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	t.Cleanup(pool.Close)

	return NewMediaRepository(pool), pool
}

var start = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

func newRecord(owner string, createdAt time.Time) media.Media {
	id := uuid.NewString()

	return media.Media{
		ID:                  id,
		OwnerIdentityID:     owner,
		Purpose:             media.PurposeDriverDocument,
		DeclaredContentType: media.TypePNG,
		DeclaredSize:        100,
		ObjectKey:           "driver_document/2026/09/" + id,
		CreatedAt:           createdAt,
	}
}

func readyInput(at time.Time) media.ReadyInput {
	return media.ReadyInput{
		ContentType: media.TypePNG,
		SizeBytes:   90,
		SHA256:      strings.Repeat("a", 64),
		Width:       10,
		Height:      20,
		At:          at,
	}
}

func TestRepositoryLifecycle(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()
	owner := uuid.NewString()
	record := newRecord(owner, start)

	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.FindByID(ctx, record.ID)
	if err != nil || found.Status != media.StatusPending || found.OwnerIdentityID != owner ||
		found.SHA256 != "" || found.CompletedAt != nil || found.UploadCleared || found.Held {
		t.Fatalf("FindByID: %+v %v", found, err)
	}

	if count, err := repo.CountPending(ctx, owner); err != nil || count != 1 {
		t.Fatalf("CountPending: %d %v", count, err)
	}

	if _, err := repo.SetHeld(ctx, record.ID, true); !errors.Is(err, media.ErrInvalidState) {
		t.Fatalf("holding a pending record: %v", err)
	}

	ready, err := repo.MarkReady(ctx, record.ID, readyInput(start.Add(time.Minute)))
	if err != nil || ready.Status != media.StatusReady || ready.SizeBytes != 90 || ready.Width != 10 ||
		ready.CompletedAt == nil || !ready.CompletedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("MarkReady: %+v %v", ready, err)
	}

	if _, err := repo.MarkReady(ctx, record.ID, readyInput(start)); !errors.Is(err, media.ErrInvalidState) {
		t.Fatalf("MarkReady twice: %v", err)
	}

	if _, err := repo.MarkRejected(ctx, record.ID, "x", start); !errors.Is(err, media.ErrInvalidState) {
		t.Fatalf("rejecting a ready record: %v", err)
	}

	if held, err := repo.SetHeld(ctx, record.ID, true); err != nil || !held.Held {
		t.Fatalf("SetHeld: %+v %v", held, err)
	}

	if _, err := repo.MarkDeleted(ctx, record.ID, start); !errors.Is(err, media.ErrInvalidState) {
		t.Fatalf("deleting a held record: %v", err)
	}

	if _, err := repo.SetHeld(ctx, record.ID, false); err != nil {
		t.Fatal(err)
	}

	if deleted, err := repo.MarkDeleted(ctx, record.ID, start); err != nil || deleted.Status != media.StatusDeleted {
		t.Fatalf("MarkDeleted: %+v %v", deleted, err)
	}

	if _, err := repo.MarkDeleted(ctx, record.ID, start); !errors.Is(err, media.ErrInvalidState) {
		t.Fatalf("MarkDeleted twice: %v", err)
	}

	if _, err := repo.FindByID(ctx, uuid.NewString()); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("FindByID of a missing record: %v", err)
	}

	if _, err := repo.MarkReady(ctx, uuid.NewString(), readyInput(start)); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("MarkReady of a missing record: %v", err)
	}
}

func TestRepositoryRejectedAndExpired(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()
	owner := uuid.NewString()

	rejected := newRecord(owner, start)
	stale := newRecord(owner, start.Add(time.Minute))
	fresh := newRecord(owner, start.Add(time.Hour))

	for _, r := range []media.Media{rejected, stale, fresh} {
		if err := repo.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	if got, err := repo.MarkRejected(ctx, rejected.ID, "wrong type", start); err != nil ||
		got.Status != media.StatusRejected || got.RejectionReason != "wrong type" {
		t.Fatalf("MarkRejected: %+v %v", got, err)
	}

	list, err := repo.ListStalePending(ctx, start.Add(30*time.Minute), 10)
	if err != nil || len(list) != 1 || list[0].ID != stale.ID {
		t.Fatalf("ListStalePending: %+v %v", list, err)
	}

	if err := repo.MarkExpired(ctx, stale.ID, start); err != nil {
		t.Fatal(err)
	}

	if err := repo.MarkExpired(ctx, stale.ID, start); !errors.Is(err, media.ErrInvalidState) {
		t.Fatalf("MarkExpired twice: %v", err)
	}

	expired, _ := repo.FindByID(ctx, stale.ID)
	if expired.Status != media.StatusExpired || !expired.UploadCleared {
		t.Fatalf("expired: %+v", expired)
	}

	if count, _ := repo.CountPending(ctx, owner); count != 1 {
		t.Fatalf("pending: %d", count)
	}

	// Only the rejected one needs clearing: the expired one was cleared when
	// it expired, and the fresh one is still pending.
	toClear, err := repo.ListUploadsToClear(ctx, start.Add(2*time.Hour), 10)
	if err != nil || len(toClear) != 1 || toClear[0].ID != rejected.ID {
		t.Fatalf("ListUploadsToClear: %+v %v", toClear, err)
	}

	if err := repo.MarkUploadCleared(ctx, rejected.ID); err != nil {
		t.Fatal(err)
	}

	if toClear, _ := repo.ListUploadsToClear(ctx, start.Add(2*time.Hour), 10); len(toClear) != 0 {
		t.Fatalf("still to clear: %+v", toClear)
	}
}

func TestRepositoryTransitionsHaveOneWinner(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()
	record := newRecord(uuid.NewString(), start)

	if err := repo.Create(ctx, record); err != nil {
		t.Fatal(err)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			var err error
			if i%2 == 0 {
				_, err = repo.MarkReady(ctx, record.ID, readyInput(start))
			} else {
				_, err = repo.MarkRejected(ctx, record.ID, "no", start)
			}

			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, media.ErrInvalidState) {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d transitions won, want 1", wins)
	}
}

func TestSchemaRefusesInconsistentRows(t *testing.T) {
	_, pool := testRepository(t)
	ctx := context.Background()

	bad := []string{
		// ready without a checksum
		`INSERT INTO media_objects (id, owner_identity_id, purpose, status, declared_content_type, declared_size, object_key, created_at)
		 VALUES (gen_random_uuid(), gen_random_uuid(), 'driver_document', 'ready', 'image/png', 1, 'k1', now())`,
		// held while pending
		`INSERT INTO media_objects (id, owner_identity_id, purpose, declared_content_type, declared_size, object_key, held, created_at)
		 VALUES (gen_random_uuid(), gen_random_uuid(), 'driver_document', 'image/png', 1, 'k2', TRUE, now())`,
		// unknown purpose
		`INSERT INTO media_objects (id, owner_identity_id, purpose, declared_content_type, declared_size, object_key, created_at)
		 VALUES (gen_random_uuid(), gen_random_uuid(), 'avatar', 'image/png', 1, 'k3', now())`,
		// empty file
		`INSERT INTO media_objects (id, owner_identity_id, purpose, declared_content_type, declared_size, object_key, created_at)
		 VALUES (gen_random_uuid(), gen_random_uuid(), 'driver_document', 'image/png', 0, 'k4', now())`,
	}

	for _, statement := range bad {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Errorf("accepted: %s", statement)
		}
	}
}
