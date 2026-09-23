package postgres

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	TRIP_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply every migration's Up section themselves (psql must be on PATH)
// and drop everything afterwards. Without the variable they are skipped.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TRIP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TRIP_TEST_DATABASE_URL is not set")
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
		psql(`DROP TABLE IF EXISTS trip_ratings, trip_offers, sos_alerts, trip_waypoints, outbox_events, trips CASCADE;`)
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

func TestTripsKeepTheirAddresses(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	photo := uuid.NewString()

	created, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:        trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff:       trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		PickupAddress: "Home, Gulan Street", DropoffAddress: "Family Mall",
		PickupDetails: "Floor 2", PickupNote: "Blue gate", PickupPhotoMediaID: photo,
	})
	if err != nil {
		t.Fatal(err)
	}

	if created.PickupAddress != "Home, Gulan Street" || created.DropoffAddress != "Family Mall" ||
		created.PickupDetails != "Floor 2" || created.PickupNote != "Blue gate" || created.PickupPhotoMediaID != photo {
		t.Fatalf("created: %+v", created)
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil || found.PickupNote != "Blue gate" || found.PickupPhotoMediaID != photo {
		t.Fatalf("found: %+v %v", found, err)
	}

	plain, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
	})
	if err != nil || plain.PickupPhotoMediaID != "" || plain.PickupAddress != "" {
		t.Fatalf("a trip without addresses: %+v %v", plain, err)
	}
}

func TestRecentDestinations(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	rider := uuid.NewString()

	// Oldest first. Two trips end within a few metres of the mall; the newest
	// one's address wins. One trip is cancelled and one is another rider's.
	add := func(lat, lng float64, address, status string, minutesAgo int, owner string) {
		t.Helper()

		_, err := pool.Exec(ctx, `
			INSERT INTO trips (id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			                   dropoff_latitude, dropoff_longitude, dropoff_address, completed_at, cancelled_at)
			VALUES ($1, $2, CASE WHEN $3 = 'completed' THEN gen_random_uuid() END, $3, 36, 44, $4, $5, $6,
			        CASE WHEN $3 = 'completed' THEN now() - make_interval(mins => $7) END,
			        CASE WHEN $3 = 'cancelled' THEN now() END)`,
			uuid.NewString(), owner, status, lat, lng, address, minutesAgo)
		if err != nil {
			t.Fatal(err)
		}
	}

	add(36.20720, 44.02260, "Family Mall (old)", "completed", 50, rider)
	add(36.23760, 43.96320, "Airport", "completed", 40, rider)
	add(36.20721, 44.02262, "Family Mall", "completed", 30, rider)
	add(36.19000, 44.01000, "Citadel", "cancelled", 20, rider)
	add(36.30000, 44.30000, "Someone else's", "completed", 10, uuid.NewString())

	got, err := repo.RecentDestinations(ctx, rider, 5)
	if err != nil {
		t.Fatal(err)
	}

	addresses := []string{}
	for _, d := range got {
		addresses = append(addresses, d.Address)
	}

	if strings.Join(addresses, ",") != "Family Mall,Airport" {
		t.Fatalf("got %v", addresses)
	}

	if one, _ := repo.RecentDestinations(ctx, rider, 1); len(one) != 1 || one[0].Address != "Family Mall" {
		t.Fatalf("limit 1: %+v", one)
	}
}
