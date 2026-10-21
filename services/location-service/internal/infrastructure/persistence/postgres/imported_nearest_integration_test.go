package postgres

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

func TestTheNearestImportedPlaceWithinADistance(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewImportedPlaceStore(pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO imported_places (id, name, kind, address, location, source) VALUES
		('near', 'Citadel Cafe', 'restaurant', 'Citadel Road', ST_GeogFromText('SRID=4326;POINT(44.0093 36.1912)'), 'test'),
		('next', 'Citadel Gate', 'landmark', '', ST_GeogFromText('SRID=4326;POINT(44.0100 36.1915)'), 'test'),
		('far',  'Airport', 'airport', '', ST_GeogFromText('SRID=4326;POINT(43.9633 36.2377)'), 'test')`); err != nil {
		t.Fatal(err)
	}

	pin := maps.Coordinates{Latitude: 36.1911, Longitude: 44.0092}

	got, found, err := store.NearestImported(ctx, pin, 150)
	if err != nil || !found || got.ID != "imported/near" || got.DisplayName != "Citadel Cafe, Citadel Road" {
		t.Fatalf("got %+v, %v, %v", got, found, err)
	}

	if _, found, err := store.NearestImported(ctx, maps.Coordinates{Latitude: 36.30, Longitude: 44.20}, 150); err != nil || found {
		t.Fatalf("nothing within 150 m: %v, %v", found, err)
	}
}
