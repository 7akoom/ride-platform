package place

import (
	"context"
	"errors"
	"testing"
)

type recordingRepository struct {
	created  []Details
	filter   Filter
	listRows int
	matches  []Match
}

func (r *recordingRepository) Create(_ context.Context, id, cityID string, d Details) (Place, error) {
	r.created = append(r.created, d)

	return Place{ID: id, CityID: cityID, Category: d.Category, Name: d.Name, Active: true}, nil
}

func (r *recordingRepository) Update(_ context.Context, id string, d Details) (Place, error) {
	return Place{ID: id, Name: d.Name}, nil
}

func (r *recordingRepository) SetActive(_ context.Context, id string, active bool) (Place, error) {
	return Place{ID: id, Active: active}, nil
}

func (r *recordingRepository) Get(_ context.Context, id string) (Place, error) {
	return Place{ID: id, Active: false}, nil
}

func (r *recordingRepository) List(_ context.Context, filter Filter) ([]Place, error) {
	r.filter = filter

	return make([]Place, r.listRows), nil
}

func (r *recordingRepository) Search(context.Context, string, *Coordinates, int) ([]Match, error) {
	return r.matches, nil
}

type fixedID string

func (f fixedID) NewID() string { return string(f) }

const (
	cityID  = "c17e0000-0000-4000-8000-000000000001"
	placeID = "91ace000-0000-4000-8000-000000000001"
)

func airport() Details {
	return Details{
		Category:    CategoryAirport,
		Name:        "Erbil International Airport",
		Names:       map[string]string{"ar": "مطار أربيل الدولي"},
		Address:     " Departures gate ",
		Coordinates: Coordinates{Latitude: 36.2376, Longitude: 43.9632},
		Priority:    100,
	}
}

func TestCreateValidates(t *testing.T) {
	cases := map[string]struct {
		city   string
		change func(*Details)
		want   error
	}{
		"no city":           {"", func(*Details) {}, ErrCityRequired},
		"malformed city":    {"erbil", func(*Details) {}, ErrCityNotFound},
		"unknown category":  {cityID, func(d *Details) { d.Category = "casino" }, ErrInvalidCategory},
		"no point":          {cityID, func(d *Details) { d.Coordinates = Coordinates{} }, ErrPointRequired},
		"bad longitude":     {cityID, func(d *Details) { d.Coordinates.Longitude = 181 }, ErrInvalidLongitude},
		"priority too high": {cityID, func(d *Details) { d.Priority = 1001 }, ErrInvalidPriority},
		"address too long": {cityID, func(d *Details) {
			d.Address = string(make([]rune, MaxAddressLength+1))
		}, ErrAddressTooLong},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepository{}
			details := airport()
			tc.change(&details)

			_, err := NewService(repo, fixedID(placeID)).Create(context.Background(), tc.city, details)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(repo.created) != 0 {
				t.Fatal("a refused place must not be stored")
			}
		})
	}
}

func TestCreateTrims(t *testing.T) {
	repo := &recordingRepository{}

	if _, err := NewService(repo, fixedID(placeID)).Create(context.Background(), cityID, airport()); err != nil {
		t.Fatal(err)
	}

	if repo.created[0].Address != "Departures gate" {
		t.Fatalf("address %q", repo.created[0].Address)
	}
}

func TestListPagesAndLimits(t *testing.T) {
	repo := &recordingRepository{listRows: DefaultPageSize + 1}
	svc := NewService(repo, fixedID(placeID))

	page, next, err := svc.List(context.Background(), ListInput{CityID: cityID})
	if err != nil {
		t.Fatal(err)
	}

	if len(page) != DefaultPageSize || next == "" || repo.filter.Limit != DefaultPageSize || repo.filter.IncludeInactive {
		t.Fatalf("page %d next %q filter %+v", len(page), next, repo.filter)
	}

	repo.listRows = 3
	if _, last, err := svc.List(context.Background(), ListInput{PageToken: next, PageSize: 500}); err != nil ||
		last != "" || repo.filter.Offset != DefaultPageSize || repo.filter.Limit != MaxPageSize {
		t.Fatalf("second page: %q %v %+v", last, err, repo.filter)
	}

	for _, bad := range []string{"x", encodePageToken(-1), "bzo5OTk5OTk5OTk"} {
		if _, _, err := svc.List(context.Background(), ListInput{PageToken: bad}); !errors.Is(err, ErrInvalidPageToken) {
			t.Fatalf("token %q: %v", bad, err)
		}
	}

	if _, _, err := svc.List(context.Background(), ListInput{Category: "casino"}); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("category: %v", err)
	}
}

func TestInactivePlaceIsHiddenFromUsers(t *testing.T) {
	svc := NewService(&recordingRepository{}, fixedID(placeID))

	if _, err := svc.Get(context.Background(), placeID, false); !errors.Is(err, ErrPlaceNotFound) {
		t.Fatalf("got %v", err)
	}

	if _, err := svc.Get(context.Background(), placeID, true); err != nil {
		t.Fatalf("staff: %v", err)
	}
}

func TestSearchCuratedNamesInTheAskedLanguage(t *testing.T) {
	repo := &recordingRepository{matches: []Match{{Place: Place{
		ID: placeID, CityName: "Erbil", CityNames: map[string]string{"ku": "هەولێر"},
		Category: CategoryAirport, Name: "Erbil Airport", Names: map[string]string{"ar": "مطار أربيل"},
		Address: "Gate 1", Coordinates: Coordinates{Latitude: 36.2, Longitude: 43.9},
	}}}}

	places, err := NewService(repo, fixedID(placeID)).SearchCurated(context.Background(), "airport", nil, 5, []string{"ku", "ar", "en"})
	if err != nil || len(places) != 1 {
		t.Fatalf("%+v %v", places, err)
	}

	got := places[0]
	if got.ID != "curated/"+placeID || got.CuratedPlaceID != placeID || got.Name != "مطار أربيل" ||
		got.DisplayName != "مطار أربيل, Gate 1, هەولێر" || got.Category != "airport" {
		t.Fatalf("got %+v", got)
	}
}
