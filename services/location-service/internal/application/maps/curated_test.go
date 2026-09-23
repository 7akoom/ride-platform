package maps

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
)

type fakeCurated struct {
	places    []Place
	err       error
	languages []string
	limit     int
}

func (f *fakeCurated) SearchCurated(_ context.Context, _ string, _ *Coordinates, limit int, languages []string) ([]Place, error) {
	f.limit, f.languages = limit, languages

	return f.places, f.err
}

func curatedRig(curated *fakeCurated, geocoder *fakeGeocoder) Service {
	return NewServiceWithCurated(&fakeRouter{}, geocoder, curated, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var (
	airport     = Place{ID: "curated/a", Name: "Erbil Airport", Coordinates: Coordinates{Latitude: 36.2376, Longitude: 43.9632}, CuratedPlaceID: "a"}
	osmAirport  = Place{ID: "way/1", Name: "Erbil International Airport", Coordinates: Coordinates{Latitude: 36.2379, Longitude: 43.9634}}
	osmElsewere = Place{ID: "way/2", Name: "Airport Road", Coordinates: Coordinates{Latitude: 36.20, Longitude: 44.00}}
)

func TestCuratedPlacesComeFirstAndHideTheSamePlaceFromTheMap(t *testing.T) {
	curated := &fakeCurated{places: []Place{airport}}
	geocoder := &fakeGeocoder{places: []Place{osmAirport, osmElsewere}}

	got, err := curatedRig(curated, geocoder).SearchPlaces(context.Background(), SearchInput{Query: "airport", Language: "ku"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].ID != "curated/a" || got[1].ID != "way/2" {
		t.Fatalf("got %+v", got)
	}

	if len(curated.languages) != 3 || curated.languages[0] != "ku" {
		t.Fatalf("languages %v", curated.languages)
	}
}

func TestMergedResultsNeverExceedTheLimit(t *testing.T) {
	curated := &fakeCurated{places: []Place{airport, airport, airport}}
	geocoder := &fakeGeocoder{places: []Place{osmElsewere, osmElsewere}}

	got, _ := curatedRig(curated, geocoder).SearchPlaces(context.Background(), SearchInput{Query: "airport", Limit: 4})
	if len(got) != 4 {
		t.Fatalf("got %d", len(got))
	}
}

func TestOneSourceFailingStillAnswers(t *testing.T) {
	down := errors.Join(ErrUnavailable)

	got, err := curatedRig(&fakeCurated{places: []Place{airport}}, &fakeGeocoder{err: down}).
		SearchPlaces(context.Background(), SearchInput{Query: "airport"})
	if err != nil || len(got) != 1 {
		t.Fatalf("map down: %+v %v", got, err)
	}

	got, err = curatedRig(&fakeCurated{err: errors.New("db down")}, &fakeGeocoder{places: []Place{osmElsewere}}).
		SearchPlaces(context.Background(), SearchInput{Query: "airport"})
	if err != nil || len(got) != 1 {
		t.Fatalf("curated down: %+v %v", got, err)
	}

	if _, err := curatedRig(&fakeCurated{err: errors.New("db down")}, &fakeGeocoder{err: down}).
		SearchPlaces(context.Background(), SearchInput{Query: "airport"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("both down: %v", err)
	}
}

func TestDistanceMeters(t *testing.T) {
	// Erbil citadel to Erbil airport: about 6.5 km.
	got := DistanceMeters(Coordinates{Latitude: 36.1912, Longitude: 44.0092}, Coordinates{Latitude: 36.2376, Longitude: 43.9632})
	if math.Abs(got-6540) > 400 {
		t.Fatalf("got %.0f m", got)
	}
}
