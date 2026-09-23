package city

import (
	"context"
	"errors"
	"testing"
)

type memoryCities struct {
	byID map[string]City
}

func (m *memoryCities) Create(_ context.Context, id string, d Details) (City, error) {
	for _, c := range m.byID {
		if c.Name == d.Name {
			return City{}, ErrCityNameTaken
		}
	}

	c := City{ID: id, Name: d.Name, Names: d.Names, TimeZone: d.TimeZone, Center: d.Center, Active: true}
	m.byID[id] = c

	return c, nil
}

func (m *memoryCities) Update(_ context.Context, id string, d Details) (City, error) {
	c, ok := m.byID[id]
	if !ok {
		return City{}, ErrCityNotFound
	}

	c.Name, c.Names, c.TimeZone, c.Center = d.Name, d.Names, d.TimeZone, d.Center
	m.byID[id] = c

	return c, nil
}

func (m *memoryCities) SetActive(_ context.Context, id string, active bool) (City, error) {
	c, ok := m.byID[id]
	if !ok {
		return City{}, ErrCityNotFound
	}

	c.Active = active
	m.byID[id] = c

	return c, nil
}

func (m *memoryCities) Get(_ context.Context, id string) (City, error) {
	c, ok := m.byID[id]
	if !ok {
		return City{}, ErrCityNotFound
	}

	return c, nil
}

func (m *memoryCities) List(_ context.Context, includeInactive bool) ([]City, error) {
	out := []City{}

	for _, c := range m.byID {
		if c.Active || includeInactive {
			out = append(out, c)
		}
	}

	return out, nil
}

type fixedID string

func (f fixedID) NewID() string { return string(f) }

const erbilID = "c17e0000-0000-4000-8000-000000000001"

func newTestService() (*Service, *memoryCities) {
	repo := &memoryCities{byID: map[string]City{}}

	return NewService(repo, fixedID(erbilID)), repo
}

func erbil() Details {
	return Details{
		Name:     "Erbil",
		Names:    map[string]string{"ar": " أربيل ", "ku": "هەولێر", "en": ""},
		TimeZone: "Asia/Baghdad",
		Center:   Coordinates{Latitude: 36.19, Longitude: 44.01},
	}
}

func TestCreateCleansAndStores(t *testing.T) {
	svc, _ := newTestService()

	created, err := svc.Create(context.Background(), erbil())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID != erbilID || created.Names["ar"] != "أربيل" || created.TimeZone != "Asia/Baghdad" {
		t.Fatalf("got %+v", created)
	}

	if _, has := created.Names["en"]; has {
		t.Fatal("an empty translation must be dropped")
	}
}

func TestCreateValidates(t *testing.T) {
	cases := map[string]struct {
		change func(*Details)
		want   error
	}{
		"no name":               {func(d *Details) { d.Name = "  " }, nil},
		"unknown language":      {func(d *Details) { d.Names = map[string]string{"fr": "Erbil"} }, nil},
		"no time zone":          {func(d *Details) { d.TimeZone = "" }, ErrTimeZoneRequired},
		"made-up time zone":     {func(d *Details) { d.TimeZone = "Mars/Olympus" }, ErrUnknownTimeZone},
		"Local":                 {func(d *Details) { d.TimeZone = "Local" }, ErrUnknownTimeZone},
		"a path":                {func(d *Details) { d.TimeZone = "../../etc/passwd" }, ErrUnknownTimeZone},
		"no center":             {func(d *Details) { d.Center = Coordinates{} }, ErrCenterRequired},
		"latitude out of range": {func(d *Details) { d.Center.Latitude = 91 }, ErrInvalidLatitude},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo := newTestService()
			details := erbil()
			tc.change(&details)

			_, err := svc.Create(context.Background(), details)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(repo.byID) != 0 {
				t.Fatal("a refused city must not be stored")
			}
		})
	}
}

func TestInactiveCityIsHiddenFromUsers(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.Create(ctx, erbil()); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SetActive(ctx, erbilID, false); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, erbilID, false); !errors.Is(err, ErrCityNotFound) {
		t.Fatalf("users: got %v", err)
	}

	if c, err := svc.Get(ctx, erbilID, true); err != nil || c.Active {
		t.Fatalf("staff: got %+v %v", c, err)
	}

	if list, _ := svc.List(ctx, false); len(list) != 0 {
		t.Fatal("an inactive city is not listed for users")
	}
}

func TestIDsAreChecked(t *testing.T) {
	svc, _ := newTestService()

	if _, err := svc.Get(context.Background(), " ", false); !errors.Is(err, ErrCityIDRequired) {
		t.Fatalf("got %v", err)
	}

	if _, err := svc.Update(context.Background(), "Erbil", erbil()); !errors.Is(err, ErrCityNotFound) {
		t.Fatalf("got %v", err)
	}
}
