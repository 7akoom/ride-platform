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

	"github.com/7akoom/ride-platform/services/location-service/internal/application/city"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/place"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
)

// These tests run against a real, EMPTY, throw-away PostGIS database:
//
//	LOCATION_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply every migration's Up section themselves (psql must be on PATH)
// and drop everything afterwards. Without the variable they are skipped.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("LOCATION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LOCATION_TEST_DATABASE_URL is not set")
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

	var ups []string

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		up, _, _ := strings.Cut(string(raw), "-- +goose Down")
		ups = append(ups, up)
	}

	dropAll := func() {
		psql("DROP TABLE IF EXISTS curated_places, zones, cities CASCADE;")
	}

	dropAll()

	for _, up := range ups {
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

func triangle(lat, lng float64) []zone.Coordinates {
	return []zone.Coordinates{
		{Latitude: lat, Longitude: lng},
		{Latitude: lat, Longitude: lng + 0.1},
		{Latitude: lat + 0.1, Longitude: lng + 0.05},
	}
}

func TestCitiesAndZones(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cities := NewCityStore(pool)
	zones := NewZoneStore(pool)

	erbil, err := cities.Create(ctx, uuid.NewString(), city.Details{
		Name: "Erbil", Names: map[string]string{"ar": "أربيل"}, TimeZone: "Asia/Baghdad",
		Center: city.Coordinates{Latitude: 36.19, Longitude: 44.01},
	})
	if err != nil || erbil.Names["ar"] != "أربيل" || !erbil.Active {
		t.Fatalf("create city: %+v %v", erbil, err)
	}

	if _, err := cities.Create(ctx, uuid.NewString(), city.Details{
		Name: "ERBIL", TimeZone: "UTC", Center: city.Coordinates{Latitude: 1, Longitude: 1},
	}); !errors.Is(err, city.ErrCityNameTaken) {
		t.Fatalf("same name: %v", err)
	}

	if _, err := zones.Create(ctx, zone.CreateInput{ID: uuid.NewString(), CityID: uuid.NewString(), Name: "X", Boundary: triangle(36, 44)}); !errors.Is(err, zone.ErrCityNotFound) {
		t.Fatalf("zone in an unknown city: %v", err)
	}

	center, err := zones.Create(ctx, zone.CreateInput{ID: uuid.NewString(), CityID: erbil.ID, Name: "Center", Boundary: triangle(36.1, 44.0)})
	if err != nil || center.CityID != erbil.ID || center.City != "Erbil" || center.TimeZone != "Asia/Baghdad" {
		t.Fatalf("create zone: %+v %v", center, err)
	}

	found, served, err := zones.FindContaining(ctx, zone.Coordinates{Latitude: 36.12, Longitude: 44.05})
	if err != nil || !served || found.ID != center.ID || found.TimeZone != "Asia/Baghdad" {
		t.Fatalf("find: %+v %v %v", found, served, err)
	}

	// Switching the city off stops serving every zone in it.
	if _, err := cities.SetActive(ctx, erbil.ID, false); err != nil {
		t.Fatal(err)
	}

	if _, served, _ := zones.FindContaining(ctx, zone.Coordinates{Latitude: 36.12, Longitude: 44.05}); served {
		t.Fatal("a zone of an inactive city is served")
	}

	if active, _ := cities.List(ctx, false); len(active) != 0 {
		t.Fatalf("active cities: %+v", active)
	}

	if all, _ := cities.List(ctx, true); len(all) != 1 {
		t.Fatalf("all cities: %+v", all)
	}

	if list, err := zones.List(ctx, erbil.ID); err != nil || len(list) != 1 {
		t.Fatalf("zones of the city: %+v %v", list, err)
	}

	renamed, err := cities.Update(ctx, erbil.ID, city.Details{
		Name: "Hewlêr", TimeZone: "Asia/Baghdad", Center: city.Coordinates{Latitude: 36.2, Longitude: 44},
	})
	if err != nil || renamed.Name != "Hewlêr" || len(renamed.Names) != 0 {
		t.Fatalf("update city: %+v %v", renamed, err)
	}

	if z, _ := zones.Get(ctx, center.ID); z.City != "Hewlêr" {
		t.Fatalf("the zone reads the city's new name: %+v", z)
	}

	if _, err := cities.Update(ctx, uuid.NewString(), city.Details{Name: "X", TimeZone: "UTC"}); !errors.Is(err, city.ErrCityNotFound) {
		t.Fatalf("update unknown: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM cities WHERE id = $1`, erbil.ID); err == nil {
		t.Fatal("a city with zones must not be deletable")
	}
}

func TestCuratedPlaces(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cities := NewCityStore(pool)
	places := NewPlaceStore(pool)

	erbil, err := cities.Create(ctx, uuid.NewString(), city.Details{
		Name: "Erbil", Names: map[string]string{"ku": "هەولێر"}, TimeZone: "Asia/Baghdad",
		Center: city.Coordinates{Latitude: 36.19, Longitude: 44.01},
	})
	if err != nil {
		t.Fatal(err)
	}

	add := func(category place.Category, name string, names map[string]string, lat, lng float64, priority int) place.Place {
		t.Helper()

		created, err := places.Create(ctx, uuid.NewString(), erbil.ID, place.Details{
			Category: category, Name: name, Names: names, Address: "Gate 1",
			Coordinates: place.Coordinates{Latitude: lat, Longitude: lng}, Priority: priority,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}

		return created
	}

	airport := add(place.CategoryAirport, "Erbil International Airport", map[string]string{"ar": "مطار أربيل الدولي"}, 36.2376, 43.9632, 100)
	mall := add(place.CategoryMall, "Family Mall", map[string]string{"ar": "فاميلي مول"}, 36.2072, 44.0226, 10)
	add(place.CategoryMall, "Majidi Mall", nil, 36.2150, 43.9850, 5)

	if airport.CityName != "Erbil" || airport.CityNames["ku"] != "هەولێر" || airport.Coordinates.Latitude != 36.2376 {
		t.Fatalf("created: %+v", airport)
	}

	if _, err := places.Create(ctx, uuid.NewString(), uuid.NewString(), place.Details{
		Category: place.CategoryMall, Name: "X", Coordinates: place.Coordinates{Latitude: 1, Longitude: 1},
	}); !errors.Is(err, place.ErrCityNotFound) {
		t.Fatalf("unknown city: %v", err)
	}

	// Search: by any language, a contained name first, then by distance.
	byArabic, err := places.Search(ctx, "مطار", nil, 5)
	if err != nil || len(byArabic) != 1 || byArabic[0].Place.ID != airport.ID {
		t.Fatalf("arabic: %+v %v", byArabic, err)
	}

	near := &place.Coordinates{Latitude: 36.2072, Longitude: 44.0226}

	malls, err := places.Search(ctx, "MALL", near, 5)
	if err != nil || len(malls) != 2 || malls[0].Place.ID != mall.ID || malls[0].DistanceMeters > 1 {
		t.Fatalf("malls: %+v %v", malls, err)
	}

	if typo, _ := places.Search(ctx, "famly mall", nil, 5); len(typo) == 0 || typo[0].Place.ID != mall.ID {
		t.Fatalf("a near miss should still find it: %+v", typo)
	}

	if wild, _ := places.Search(ctx, "%", nil, 5); len(wild) != 0 {
		t.Fatalf("%% must be a literal character: %+v", wild)
	}

	// Inactive places and places of inactive cities disappear for users.
	if _, err := places.SetActive(ctx, mall.ID, false); err != nil {
		t.Fatal(err)
	}

	if found, _ := places.Search(ctx, "family", nil, 5); len(found) != 0 {
		t.Fatalf("an inactive place is found: %+v", found)
	}

	page, err := places.List(ctx, place.Filter{CityID: erbil.ID, Limit: 10})
	if err != nil || len(page) != 2 || page[0].ID != airport.ID {
		t.Fatalf("list: %+v %v", page, err)
	}

	if all, _ := places.List(ctx, place.Filter{IncludeInactive: true, Limit: 10}); len(all) != 3 {
		t.Fatalf("admin list: %d", len(all))
	}

	if onePage, _ := places.List(ctx, place.Filter{Category: place.CategoryAirport, Limit: 1}); len(onePage) != 1 {
		t.Fatalf("by category: %d", len(onePage))
	}

	if _, err := cities.SetActive(ctx, erbil.ID, false); err != nil {
		t.Fatal(err)
	}

	if found, _ := places.Search(ctx, "airport", nil, 5); len(found) != 0 {
		t.Fatal("a place of an inactive city is found")
	}

	if got, _ := places.Get(ctx, airport.ID); got.Active {
		t.Fatal("a place of an inactive city reads as active")
	}

	updated, err := places.Update(ctx, airport.ID, place.Details{
		Category: place.CategoryAirport, Name: "EBL", Coordinates: place.Coordinates{Latitude: 36.24, Longitude: 43.96}, Priority: 1,
	})
	if err != nil || updated.Name != "EBL" || updated.Coordinates.Latitude != 36.24 || len(updated.Names) != 0 {
		t.Fatalf("update: %+v %v", updated, err)
	}

	if _, err := places.Get(ctx, uuid.NewString()); !errors.Is(err, place.ErrPlaceNotFound) {
		t.Fatalf("get unknown: %v", err)
	}
}
