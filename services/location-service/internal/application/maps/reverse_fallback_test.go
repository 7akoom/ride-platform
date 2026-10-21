package maps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

// nearbyImported can also say which imported place is nearest.
type nearbyImported struct {
	fakeImported

	nearest Place
	found   bool
	err     error
	within  float64
}

func (f *nearbyImported) NearestImported(_ context.Context, _ Coordinates, within float64) (Place, bool, error) {
	f.within = within

	return f.nearest, f.found, f.err
}

func reverseRig(geocoder *fakeGeocoder, imported *nearbyImported) Service {
	return NewServiceWithSources(&fakeRouter{}, geocoder, &fakeCurated{}, imported, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var pin = Coordinates{Latitude: 36.1911, Longitude: 44.0092}

func TestWhenTheMapIsDownTheNearestImportedPlaceNamesThePin(t *testing.T) {
	imported := &nearbyImported{nearest: importedAt("c", "Erbil Citadel", 36.1912, 44.0093), found: true}
	geocoder := &fakeGeocoder{err: fmt.Errorf("%w: connection refused", ErrUnavailable)}

	got, err := reverseRig(geocoder, imported).ReverseGeocode(context.Background(), ReverseInput{Coordinates: &pin})
	if err != nil {
		t.Fatal(err)
	}

	if got.Name != "Erbil Citadel" || got.Coordinates != pin || imported.within != reverseFallbackMeters {
		t.Fatalf("got %+v within %v", got, imported.within)
	}
}

func TestTheMapStillAnswersFirst(t *testing.T) {
	imported := &nearbyImported{nearest: importedAt("c", "Erbil Citadel", 36.1912, 44.0093), found: true}
	geocoder := &fakeGeocoder{place: Place{ID: "way/1", Name: "Citadel Street"}}

	got, err := reverseRig(geocoder, imported).ReverseGeocode(context.Background(), ReverseInput{Coordinates: &pin})
	if err != nil || got.Name != "Citadel Street" || imported.within != 0 {
		t.Fatalf("got %+v, %v (asked imported: %v)", got, err, imported.within != 0)
	}
}

func TestTheMapsAnswerIsKeptWhenNothingIsNearOrItFoundNothing(t *testing.T) {
	down := fmt.Errorf("%w: connection refused", ErrUnavailable)

	cases := map[string]struct {
		geocoderErr error
		imported    *nearbyImported
		want        error
	}{
		"down, nothing imported nearby":   {down, &nearbyImported{}, ErrUnavailable},
		"down, the nearby search failing": {down, &nearbyImported{err: errors.New("db down")}, ErrUnavailable},
		"up, nothing at that point":       {ErrPlaceNotFound, &nearbyImported{found: true, nearest: importedAt("x", "X", 0, 0)}, ErrPlaceNotFound},
	}

	for name, c := range cases {
		_, err := reverseRig(&fakeGeocoder{err: c.geocoderErr}, c.imported).ReverseGeocode(context.Background(), ReverseInput{Coordinates: &pin})
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
