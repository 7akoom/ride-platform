package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/address"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	RIDER_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply every migration's Up section themselves (psql must be on PATH)
// and drop everything afterwards. Without the variable they are skipped.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("RIDER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("RIDER_TEST_DATABASE_URL is not set")
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
		psql(`DROP TABLE IF EXISTS saved_addresses, processed_rating_events, outbox_events, riders CASCADE;`)
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

func addRider(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()

	id, identity := uuid.NewString(), uuid.NewString()

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO riders (id, identity_id, display_name) VALUES ($1, $2, 'Test Rider')`, id, identity); err != nil {
		t.Fatal(err)
	}

	return id, identity
}

func newAddress(riderID string, kind address.Kind, label string) address.Address {
	return address.Address{
		ID: uuid.NewString(), RiderID: riderID, Kind: kind, Label: label,
		Coordinates: address.Coordinates{Latitude: 36.19, Longitude: 44.01},
		Address:     "Gulan Street", Details: "Floor 2", NoteForDriver: "Blue gate",
	}
}

func TestSavedAddresses(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAddressRepository(pool)
	rider, identity := addRider(t, pool)
	other, _ := addRider(t, pool)

	if got, err := repo.IdentityOf(ctx, rider); err != nil || got != identity {
		t.Fatalf("identity: %q %v", got, err)
	}

	if _, err := repo.IdentityOf(ctx, uuid.NewString()); !errors.Is(err, address.ErrRiderNotFound) {
		t.Fatalf("unknown rider: %v", err)
	}

	photo := uuid.NewString()
	h := newAddress(rider, address.KindHome, "")
	h.PhotoMediaID = photo

	created, err := repo.Create(ctx, h)
	if err != nil || created.PhotoMediaID != photo || created.NoteForDriver != "Blue gate" {
		t.Fatalf("create: %+v %v", created, err)
	}

	if _, err := repo.Create(ctx, newAddress(rider, address.KindHome, "")); !errors.Is(err, address.ErrKindTaken) {
		t.Fatalf("second home: %v", err)
	}

	if _, err := repo.Create(ctx, newAddress(other, address.KindHome, "")); err != nil {
		t.Fatalf("another rider's home: %v", err)
	}

	if _, err := repo.Create(ctx, newAddress(uuid.NewString(), address.KindHome, "")); !errors.Is(err, address.ErrRiderNotFound) {
		t.Fatalf("unknown rider: %v", err)
	}

	for _, label := range []string{"Zoo", "Gym"} {
		if _, err := repo.Create(ctx, newAddress(rider, address.KindOther, label)); err != nil {
			t.Fatal(err)
		}
	}

	work, err := repo.Create(ctx, newAddress(rider, address.KindWork, ""))
	if err != nil {
		t.Fatal(err)
	}

	list, err := repo.List(ctx, rider)
	if err != nil {
		t.Fatal(err)
	}

	order := []string{}
	for _, a := range list {
		order = append(order, string(a.Kind)+":"+a.Label)
	}

	if strings.Join(order, ",") != "home:,work:,other:Gym,other:Zoo" {
		t.Fatalf("order: %v", order)
	}

	if _, err := repo.Get(ctx, other, created.ID); !errors.Is(err, address.ErrAddressNotFound) {
		t.Fatalf("another rider reads it: %v", err)
	}

	// A work address cannot become a second home.
	work.Kind = address.KindHome
	if _, err := repo.Update(ctx, work); !errors.Is(err, address.ErrKindTaken) {
		t.Fatalf("work to home: %v", err)
	}

	created.PhotoMediaID = ""
	created.Label = "Mum's"

	updated, err := repo.Update(ctx, created)
	if err != nil || updated.PhotoMediaID != "" || updated.Label != "Mum's" {
		t.Fatalf("update: %+v %v", updated, err)
	}

	stolen := created
	stolen.RiderID = other

	if _, err := repo.Update(ctx, stolen); !errors.Is(err, address.ErrAddressNotFound) {
		t.Fatalf("another rider updates it: %v", err)
	}

	if _, err := repo.Delete(ctx, other, created.ID); !errors.Is(err, address.ErrAddressNotFound) {
		t.Fatalf("another rider deletes it: %v", err)
	}

	deleted, err := repo.Delete(ctx, rider, created.ID)
	if err != nil || deleted.ID != created.ID {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
}

func TestSavedAddressLimitHoldsUnderConcurrency(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAddressRepository(pool)
	rider, _ := addRider(t, pool)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		refused int
	)

	for i := 0; i < address.MaxPerRider+10; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			_, err := repo.Create(ctx, newAddress(rider, address.KindOther, fmt.Sprintf("Place %d", i)))

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				created++
			case errors.Is(err, address.ErrTooManyAddresses):
				refused++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if created != address.MaxPerRider || refused != 10 {
		t.Fatalf("created %d, refused %d", created, refused)
	}
}

func TestDeletingARiderDeletesTheirAddresses(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAddressRepository(pool)
	rider, _ := addRider(t, pool)

	if _, err := repo.Create(ctx, newAddress(rider, address.KindHome, "")); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM riders WHERE id = $1`, rider); err != nil {
		t.Fatal(err)
	}

	if list, _ := repo.List(ctx, rider); len(list) != 0 {
		t.Fatal("addresses outlived their rider")
	}
}
