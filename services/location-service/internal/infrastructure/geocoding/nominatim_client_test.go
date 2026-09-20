package geocoding

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

func nominatimServer(t *testing.T, status int, body string, seen *url.URL) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r.URL
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server
}

const searchAnswer = `[
  {"place_id":1,"osm_type":"way","osm_id":123456,"lat":"36.191100","lon":"44.009200","category":"tourism","type":"attraction",
   "name":"قلعة أربيل","display_name":"قلعة أربيل, أربيل, محافظة أربيل, العراق",
   "address":{"tourism":"قلعة أربيل","road":"شارع القلعة","city":"أربيل","state":"محافظة أربيل","postcode":"44001","country":"العراق","country_code":"iq"}},
  {"place_id":2,"osm_type":"node","osm_id":42,"lat":"36.2","lon":"44.0","category":"place","type":"house",
   "name":"","display_name":"12, شارع 60, أربيل, العراق","address":{"house_number":"12","road":"شارع 60"}},
  {"place_id":3,"osm_type":"node","osm_id":43,"lat":"not-a-number","lon":"44.0","category":"x","type":"y","name":"broken","display_name":"broken"}
]`

func TestSearchReadsPlacesAndKeepsTheUsefulPartsOfTheAddress(t *testing.T) {
	var seen url.URL

	client := NewNominatimClient(nominatimServer(t, http.StatusOK, searchAnswer, &seen).URL, "iq", time.Second)

	places, err := client.Search(context.Background(), "قلعة أربيل", nil, 5, "ar,ku,en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(places) != 2 {
		t.Fatalf("a place without a usable position must be dropped: got %d: %+v", len(places), places)
	}

	first := places[0]
	if first.ID != "way/123456" || first.Name != "قلعة أربيل" || first.Category != "tourism" || first.Type != "attraction" ||
		first.Coordinates != (maps.Coordinates{Latitude: 36.1911, Longitude: 44.0092}) {
		t.Errorf("unexpected first place: %+v", first)
	}

	if first.Address["road"] != "شارع القلعة" || first.Address["city"] != "أربيل" || first.Address["postcode"] != "44001" {
		t.Errorf("unexpected address: %+v", first.Address)
	}

	if _, leaked := first.Address["country_code"]; leaked {
		t.Errorf("only the useful parts of an address are passed on: %+v", first.Address)
	}

	if places[1].Name != "12" {
		t.Errorf("a place with no name takes the first part of its display name, got %q", places[1].Name)
	}

	query := seen.Query()
	if seen.Path != "/search" || query.Get("q") != "قلعة أربيل" || query.Get("format") != "jsonv2" || query.Get("limit") != "5" ||
		query.Get("countrycodes") != "iq" || query.Get("accept-language") != "ar,ku,en" || query.Get("addressdetails") != "1" {
		t.Errorf("unexpected request: %s", seen.String())
	}

	if query.Get("viewbox") != "" || query.Get("bounded") != "" {
		t.Errorf("no point to rank by, so no viewbox: %s", seen.String())
	}
}

func TestASearchNearAPointIsRankedTowardsItWithoutExcludingTheRest(t *testing.T) {
	var seen url.URL

	client := NewNominatimClient(nominatimServer(t, http.StatusOK, `[]`, &seen).URL, "iq", time.Second)

	if _, err := client.Search(context.Background(), "mall", &maps.Coordinates{Latitude: 36.19, Longitude: 44.01}, 3, "ar"); err != nil {
		t.Fatal(err)
	}

	query := seen.Query()
	if query.Get("viewbox") != "43.760000,36.440000,44.260000,35.940000" {
		t.Errorf("the viewbox is west,north,east,south around the point: %q", query.Get("viewbox"))
	}

	if query.Get("bounded") != "0" {
		t.Errorf("the box only ranks, it must not exclude: bounded=%q", query.Get("bounded"))
	}
}

func TestNoCountryCodesMeansNoRestriction(t *testing.T) {
	var seen url.URL

	client := NewNominatimClient(nominatimServer(t, http.StatusOK, `[]`, &seen).URL, "", time.Second)
	if _, err := client.Search(context.Background(), "erbil", nil, 3, "en"); err != nil {
		t.Fatal(err)
	}

	if seen.Query().Has("countrycodes") {
		t.Errorf("unexpected countrycodes in %s", seen.String())
	}
}

func TestNothingFoundIsAnEmptyListNotAnError(t *testing.T) {
	client := NewNominatimClient(nominatimServer(t, http.StatusOK, `[]`, nil).URL, "iq", time.Second)

	places, err := client.Search(context.Background(), "zzzzzz", nil, 5, "ar")
	if err != nil || places == nil || len(places) != 0 {
		t.Errorf("expected an empty, non-nil list: %#v, %v", places, err)
	}
}

const reverseAnswer = `{"place_id":9,"osm_type":"way","osm_id":777,"lat":"36.2286","lon":"43.9750","category":"highway","type":"residential",
  "name":"شارع عنكاوا","display_name":"شارع عنكاوا, عنكاوا, أربيل, العراق","address":{"road":"شارع عنكاوا","suburb":"عنكاوا","city":"أربيل"}}`

func TestReverseNamesWhatIsAtAPoint(t *testing.T) {
	var seen url.URL

	client := NewNominatimClient(nominatimServer(t, http.StatusOK, reverseAnswer, &seen).URL, "iq", time.Second)

	place, err := client.Reverse(context.Background(), maps.Coordinates{Latitude: 36.2286, Longitude: 43.975}, "en,ar,ku")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if place.ID != "way/777" || place.Name != "شارع عنكاوا" || place.Address["suburb"] != "عنكاوا" {
		t.Errorf("unexpected place: %+v", place)
	}

	query := seen.Query()
	if seen.Path != "/reverse" || query.Get("lat") != "36.228600" || query.Get("lon") != "43.975000" || query.Get("zoom") != "18" || query.Get("accept-language") != "en,ar,ku" {
		t.Errorf("unexpected request: %s", seen.String())
	}
}

func TestAPointWithNothingThereIsNotFoundEvenThoughNominatimAnswers200(t *testing.T) {
	client := NewNominatimClient(nominatimServer(t, http.StatusOK, `{"error":"Unable to geocode"}`, nil).URL, "iq", time.Second)

	if _, err := client.Reverse(context.Background(), maps.Coordinates{Latitude: 0, Longitude: 0}, "ar"); !errors.Is(err, maps.ErrPlaceNotFound) {
		t.Errorf("expected ErrPlaceNotFound, got %v", err)
	}
}

func TestNominatimFailuresAreUnavailableNotNotFound(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"a server error":         {http.StatusInternalServerError, `oops`},
		"a bad gateway":          {http.StatusBadGateway, ``},
		"not JSON":               {http.StatusOK, `<html>`},
		"an object for a search": {http.StatusOK, `{"error":"x"}`},
	}

	for name, tc := range cases {
		client := NewNominatimClient(nominatimServer(t, tc.status, tc.body, nil).URL, "iq", time.Second)

		if _, err := client.Search(context.Background(), "erbil", nil, 3, "ar"); !errors.Is(err, maps.ErrUnavailable) {
			t.Errorf("%s (search): expected ErrUnavailable, got %v", name, err)
		}

		if _, err := client.Reverse(context.Background(), maps.Coordinates{Latitude: 36, Longitude: 44}, "ar"); tc.status != http.StatusOK && !errors.Is(err, maps.ErrUnavailable) {
			t.Errorf("%s (reverse): expected ErrUnavailable, got %v", name, err)
		}
	}
}

func TestANominatimThatIsNotRunningIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	client := NewNominatimClient(url, "iq", time.Second)

	if _, err := client.Search(context.Background(), "erbil", nil, 3, "ar"); !errors.Is(err, maps.ErrUnavailable) {
		t.Errorf("search: expected ErrUnavailable, got %v", err)
	}

	if _, err := client.Reverse(context.Background(), maps.Coordinates{Latitude: 36, Longitude: 44}, "ar"); !errors.Is(err, maps.ErrUnavailable) {
		t.Errorf("reverse: expected ErrUnavailable, got %v", err)
	}
}

func TestATrailingSlashInTheBaseURLDoesNotBreakThePath(t *testing.T) {
	var seen url.URL

	client := NewNominatimClient(nominatimServer(t, http.StatusOK, `[]`, &seen).URL+"/", "iq", time.Second)
	if _, err := client.Search(context.Background(), "erbil", nil, 3, "ar"); err != nil {
		t.Fatal(err)
	}

	if strings.HasPrefix(seen.Path, "//") {
		t.Errorf("double slash in %q", seen.Path)
	}
}
