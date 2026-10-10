package maps

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

type fakeImported struct {
	places []Place
	err    error
}

func (f *fakeImported) SearchImported(context.Context, string, *Coordinates, int) ([]Place, error) {
	return f.places, f.err
}

func sourcesRig(curated *fakeCurated, imported *fakeImported, geocoder *fakeGeocoder) Service {
	return NewServiceWithSources(&fakeRouter{}, geocoder, curated, imported, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func importedAt(id, name string, lat, lng float64) Place {
	return Place{ID: "imported/" + id, Name: name, Coordinates: Coordinates{Latitude: lat, Longitude: lng}}
}

func ids(places []Place) []string {
	out := make([]string, len(places))
	for i, p := range places {
		out[i] = p.ID
	}

	return out
}

func TestImportedPlacesComeAfterCuratedAndBeforeTheMap(t *testing.T) {
	citadel := importedAt("c", "Erbil Citadel", 36.1912, 44.0092)
	osmCitadel := Place{ID: "way/9", Name: "Citadel", Coordinates: Coordinates{Latitude: 36.1913, Longitude: 44.0093}}
	osmStreet := Place{ID: "way/10", Name: "Citadel Street", Coordinates: Coordinates{Latitude: 36.1950, Longitude: 44.0150}}

	got, err := sourcesRig(
		&fakeCurated{places: []Place{airport}},
		&fakeImported{places: []Place{citadel, importedAt("a", "Airport Cafe", 36.2377, 43.9633)}},
		&fakeGeocoder{places: []Place{osmCitadel, osmStreet}},
	).SearchPlaces(context.Background(), SearchInput{Query: "citadel", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	// The café at the curated airport is the airport; the map's citadel is the
	// imported one; the street is its own place.
	want := []string{"curated/a", "imported/c", "way/10"}
	if len(got) != len(want) {
		t.Fatalf("got %v", ids(got))
	}

	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got %v, want %v", ids(got), want)
		}
	}
}

func TestTheMapKeepsAThirdOfTheListUntilItRunsOut(t *testing.T) {
	var many []Place
	for i := range 10 {
		many = append(many, importedAt(string(rune('a'+i)), "Cafe", 36+float64(i)/10, 44))
	}

	streets := []Place{
		{ID: "way/1", Name: "One Street", Coordinates: Coordinates{Latitude: 35, Longitude: 43}},
		{ID: "way/2", Name: "Two Street", Coordinates: Coordinates{Latitude: 35.5, Longitude: 43}},
		{ID: "way/3", Name: "Three Street", Coordinates: Coordinates{Latitude: 35.8, Longitude: 43}},
		{ID: "way/4", Name: "Four Street", Coordinates: Coordinates{Latitude: 35.9, Longitude: 43}},
	}

	got, _ := sourcesRig(&fakeCurated{}, &fakeImported{places: many}, &fakeGeocoder{places: streets}).
		SearchPlaces(context.Background(), SearchInput{Query: "cafe", Limit: 9})

	if len(got) != 9 || mapResults(got) != 3 {
		t.Fatalf("four streets: %v", ids(got))
	}

	got, _ = sourcesRig(&fakeCurated{}, &fakeImported{places: many}, &fakeGeocoder{places: streets[:1]}).
		SearchPlaces(context.Background(), SearchInput{Query: "cafe", Limit: 9})
	if len(got) != 9 || mapResults(got) != 1 {
		t.Fatalf("one street: %v", ids(got))
	}
}

func mapResults(places []Place) int {
	count := 0

	for _, p := range places {
		if strings.HasPrefix(p.ID, "way/") {
			count++
		}
	}

	return count
}

func TestImportedPlacesAnswerWhenTheMapIsDown(t *testing.T) {
	got, err := sourcesRig(
		&fakeCurated{err: errors.New("db down")},
		&fakeImported{places: []Place{importedAt("c", "Erbil Citadel", 36.19, 44.01)}},
		&fakeGeocoder{err: ErrUnavailable},
	).SearchPlaces(context.Background(), SearchInput{Query: "citadel"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", ids(got), err)
	}

	if _, err := sourcesRig(&fakeCurated{}, &fakeImported{err: errors.New("db down")}, &fakeGeocoder{err: ErrUnavailable}).
		SearchPlaces(context.Background(), SearchInput{Query: "citadel"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("all down: %v", err)
	}
}
