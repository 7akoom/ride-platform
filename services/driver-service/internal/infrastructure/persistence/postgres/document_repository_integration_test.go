package postgres

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	DRIVER_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply every migration's Up section themselves (psql must be on PATH)
// and drop everything afterwards. Without the variable they are skipped.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DRIVER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DRIVER_TEST_DATABASE_URL is not set")
	}

	files, err := filepath.Glob("../../../../migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}

	sort.Strings(files)

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	dropAll := func() {
		psql(`DROP TABLE IF EXISTS driver_documents, driver_document_types, processed_rating_events,
			outbox_events, drivers CASCADE;`)
	}

	dropAll()

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		up, _, _ := strings.Cut(string(raw), "-- +goose Down")
		psql(up)
	}

	t.Cleanup(dropAll)

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func addDriver(t *testing.T, pool *pgxpool.Pool, availability string) string {
	t.Helper()

	id := uuid.NewString()

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO drivers (id, identity_id, display_name, status, availability_status,
		    vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number)
		 VALUES ($1, $2, 'Test Driver', 'active', $3, 'Kia', 'Rio', 'Red', $4)`,
		id, uuid.NewString(), availability, "T-"+id[:8]); err != nil {
		t.Fatal(err)
	}

	return id
}

func newDoc(driverID, typeCode, number string, expiresOn documents.Date) documents.Document {
	return documents.Document{
		ID: uuid.NewString(), DriverID: driverID, TypeCode: typeCode, MediaID: uuid.NewString(),
		Number: number, ExpiresOn: expiresOn, Status: documents.StatusPending,
	}
}

func events(t *testing.T, pool *pgxpool.Pool, driverID, eventType string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(),
		`SELECT payload::text FROM outbox_events WHERE aggregate_id = $1 AND event_type = $2 ORDER BY occurred_at`,
		driverID, eventType)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []string

	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}

		out = append(out, payload)
	}

	return out
}

func availability(t *testing.T, pool *pgxpool.Pool, driverID string) string {
	t.Helper()

	var got string
	if err := pool.QueryRow(context.Background(), `SELECT availability_status FROM drivers WHERE id = $1`, driverID).Scan(&got); err != nil {
		t.Fatal(err)
	}

	return got
}

func TestDocumentTypesAreSeededAndChangeable(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	active, err := repo.ListTypes(ctx, false)
	if err != nil || len(active) != 11 || active[0].Code != "profile_photo" {
		t.Fatalf("seeded types: %d, first %+v, %v", len(active), active, err)
	}

	licence, err := repo.GetType(ctx, "driving_licence_front")
	if err != nil || !licence.RequiresNumber || !licence.RequiresExpiry || !licence.Required {
		t.Fatalf("licence: %+v %v", licence, err)
	}

	licence.Active = false
	if _, err := repo.UpsertType(ctx, licence); err != nil {
		t.Fatal(err)
	}

	if active, _ := repo.ListTypes(ctx, false); len(active) != 10 {
		t.Errorf("an inactive type is still listed: %d", len(active))
	}

	if all, _ := repo.ListTypes(ctx, true); len(all) != 11 {
		t.Errorf("staff see %d types, want 11", len(all))
	}

	if _, err := repo.GetType(ctx, "passport"); !errors.Is(err, documents.ErrTypeNotFound) {
		t.Errorf("unknown type: %v", err)
	}
}

func TestSubmitApproveRejectAndSupersede(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)
	licence, _ := repo.GetType(ctx, "driving_licence_front")
	d := addDriver(t, pool, "available")
	expiry := documents.NewDate(2030, 1, 31)

	first := newDoc(d, licence.Code, "AB123", expiry)
	if _, superseded, err := repo.Submit(ctx, first); err != nil || len(superseded) != 0 {
		t.Fatalf("submit: %v %v", superseded, err)
	}

	// Same file again: refused.
	again := newDoc(d, licence.Code, "AB123", expiry)
	again.MediaID = first.MediaID
	if _, _, err := repo.Submit(ctx, again); !errors.Is(err, documents.ErrMediaAlreadyUsed) {
		t.Fatalf("reused file: %v", err)
	}

	// A second submission replaces the pending one.
	second := newDoc(d, licence.Code, "AB124", expiry)
	_, superseded, err := repo.Submit(ctx, second)
	if err != nil || len(superseded) != 1 || superseded[0].ID != first.ID || superseded[0].Status != documents.StatusSuperseded {
		t.Fatalf("resubmit: %+v %v", superseded, err)
	}

	reviewer := uuid.NewString()

	approved, superseded, err := repo.Approve(ctx, documents.ApproveRecord{
		DocumentID: second.ID, Number: "AB125", ExpiresOn: expiry, ReviewedBy: reviewer, Type: licence,
	})
	if err != nil || approved.Status != documents.StatusApproved || approved.Number != "AB125" ||
		approved.ReviewedBy != reviewer || approved.ExpiresOn != expiry || len(superseded) != 0 {
		t.Fatalf("approve: %+v %v %v", approved, superseded, err)
	}

	if _, _, err := repo.Approve(ctx, documents.ApproveRecord{DocumentID: second.ID, Type: licence}); !errors.Is(err, documents.ErrDocumentNotPending) {
		t.Fatalf("approve twice: %v", err)
	}

	// Another driver cannot get the same licence number approved.
	other := addDriver(t, pool, "offline")
	copied := newDoc(other, licence.Code, "AB125", expiry)
	if _, _, err := repo.Submit(ctx, copied); err != nil {
		t.Fatal(err)
	}

	if _, _, err := repo.Approve(ctx, documents.ApproveRecord{DocumentID: copied.ID, Number: "AB125", ExpiresOn: expiry, Type: licence}); !errors.Is(err, documents.ErrNumberTaken) {
		t.Fatalf("same number for another driver: %v", err)
	}

	// A renewal is approved over the old one, which is superseded.
	renewal := newDoc(d, licence.Code, "AB125", documents.NewDate(2035, 1, 31))
	if _, _, err := repo.Submit(ctx, renewal); err != nil {
		t.Fatal(err)
	}

	_, superseded, err = repo.Approve(ctx, documents.ApproveRecord{DocumentID: renewal.ID, Number: "AB125", ExpiresOn: renewal.ExpiresOn, Type: licence})
	if err != nil || len(superseded) != 1 || superseded[0].ID != second.ID {
		t.Fatalf("renewal: %+v %v", superseded, err)
	}

	// Withdrawing the approved one takes the available driver offline.
	withdrawn, err := repo.Reject(ctx, documents.RejectRecord{
		DocumentID: renewal.ID, ExpectedStatus: documents.StatusApproved, Reason: "forged", TakeOffline: true, Type: licence,
	})
	if err != nil || withdrawn.Status != documents.StatusRejected || withdrawn.RejectionReason != "forged" {
		t.Fatalf("withdraw: %+v %v", withdrawn, err)
	}

	if got := availability(t, pool, d); got != "offline" {
		t.Errorf("availability after withdrawal = %s", got)
	}

	if _, err := repo.Reject(ctx, documents.RejectRecord{DocumentID: renewal.ID, ExpectedStatus: documents.StatusApproved, Reason: "x", Type: licence}); !errors.Is(err, documents.ErrDocumentNotReviewable) {
		t.Fatalf("reject twice: %v", err)
	}

	current, err := repo.ListByDriver(ctx, d, false)
	if err != nil || len(current) != 1 || current[0].ID != renewal.ID {
		t.Fatalf("current documents: %+v %v", current, err)
	}

	all, _ := repo.ListByDriver(ctx, d, true)
	if len(all) != 3 {
		t.Errorf("history has %d documents, want 3", len(all))
	}

	reviewed := events(t, pool, d, "driver.document_reviewed")
	if len(reviewed) != 3 || !strings.Contains(reviewed[2], `"withdrawn": true`) || !strings.Contains(reviewed[0], `"document_name_ar": "إجازة السوق (الوجه)"`) {
		t.Errorf("review events: %v", reviewed)
	}
}

func TestPendingQueuePagesOldestFirst(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)
	d := addDriver(t, pool, "offline")

	var ids []string

	for _, code := range []string{"profile_photo", "national_id_back", "vehicle_photo_side"} {
		doc := newDoc(d, code, "", documents.Date{})
		if _, _, err := repo.Submit(ctx, doc); err != nil {
			t.Fatal(err)
		}

		ids = append(ids, doc.ID)
	}

	page, err := repo.ListPending(ctx, documents.PendingQuery{Limit: 2})
	if err != nil || len(page) != 2 || page[0].Document.ID != ids[0] || page[0].DriverDisplayName != "Test Driver" || page[0].DriverStatus != "active" {
		t.Fatalf("first page: %+v %v", page, err)
	}

	rest, err := repo.ListPending(ctx, documents.PendingQuery{AfterCreatedAt: page[1].Document.CreatedAt, AfterID: page[1].Document.ID, Limit: 2})
	if err != nil || len(rest) != 1 || rest[0].Document.ID != ids[2] {
		t.Fatalf("second page: %+v %v", rest, err)
	}
}

func TestExpiryRemindsOnceAndTakesDriversOffline(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)
	licence, _ := repo.GetType(ctx, "driving_licence_front")
	today := documents.NewDate(2026, 9, 27)
	thresholds := []int{1, 7, 30}

	approve := func(driverID string, expiresOn documents.Date) {
		t.Helper()

		doc := newDoc(driverID, licence.Code, "L-"+driverID[:8], expiresOn)
		if _, _, err := repo.Submit(ctx, doc); err != nil {
			t.Fatal(err)
		}

		if _, _, err := repo.Approve(ctx, documents.ApproveRecord{DocumentID: doc.ID, Number: doc.Number, ExpiresOn: expiresOn, Type: licence}); err != nil {
			t.Fatal(err)
		}
	}

	soon := addDriver(t, pool, "available")
	approve(soon, today.AddDays(5))

	far := addDriver(t, pool, "available")
	approve(far, today.AddDays(90))

	ranOut := addDriver(t, pool, "available")
	approve(ranOut, today.AddDays(-1))

	busy := addDriver(t, pool, "busy")
	approve(busy, today.AddDays(-2))

	round, err := repo.RunExpiry(ctx, today, thresholds, 100)
	if err != nil || round.Reminded != 1 || round.Expired != 2 || round.TookOffline != 1 {
		t.Fatalf("first round: %+v %v", round, err)
	}

	if got := events(t, pool, soon, "driver.document_expiring"); len(got) != 1 || !strings.Contains(got[0], `"days_left": 5`) {
		t.Errorf("reminder: %v", got)
	}

	if got := availability(t, pool, ranOut); got != "offline" {
		t.Errorf("driver with an expired licence is %s", got)
	}

	// Busy is left to finish the trip; the release to available is refused later.
	if got := availability(t, pool, busy); got != "busy" {
		t.Errorf("busy driver is %s", got)
	}

	round, err = repo.RunExpiry(ctx, today, thresholds, 100)
	if err != nil || round != (documents.ExpiryRound{}) {
		t.Fatalf("same day again: %+v %v", round, err)
	}

	// Six days later the 1-day reminder is due; the 7-day one was the last sent.
	round, err = repo.RunExpiry(ctx, today.AddDays(4), thresholds, 100)
	if err != nil || round.Reminded != 1 {
		t.Fatalf("a day before: %+v %v", round, err)
	}

	if got := events(t, pool, soon, "driver.document_expiring"); len(got) != 2 || !strings.Contains(got[1], `"days_left": 1`) {
		t.Errorf("second reminder: %v", got)
	}

	if got := events(t, pool, ranOut, "driver.document_expired"); len(got) != 1 {
		t.Errorf("expired notices: %v", got)
	}
}

func TestStatusChangesAreAnnounced(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDriverRepository(pool)

	id := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO drivers (id, identity_id, display_name, vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number)
		 VALUES ($1, $2, 'New Driver', 'Kia', 'Rio', 'Red', 'NEW-1')`, id, uuid.NewString()); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateStatus(ctx, driver.UpdateStatusInput{
		DriverID: id, To: driver.StatusRejected, AllowedFrom: []driver.Status{driver.StatusPending}, Reason: "blurry photo",
	}); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := repo.UpdateStatus(ctx, driver.UpdateStatusInput{
			DriverID: id, To: driver.StatusActive, AllowedFrom: []driver.Status{driver.StatusPending, driver.StatusRejected},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if got := events(t, pool, id, "driver.rejected"); len(got) != 1 || !strings.Contains(got[0], "blurry photo") {
		t.Errorf("rejected: %v", got)
	}

	// The retried approval changed nothing and said nothing.
	if got := events(t, pool, id, "driver.approved"); len(got) != 1 {
		t.Errorf("approved: %v", got)
	}
}
