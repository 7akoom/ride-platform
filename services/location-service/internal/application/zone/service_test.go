package zone_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	createResult zone.Zone
	createErr    error
	createCalls  []zone.CreateInput

	updateResult zone.Zone
	updateErr    error

	setActiveResult zone.Zone
	setActiveErr    error

	getResult zone.Zone
	getErr    error

	listResult []zone.Zone
	listErr    error
	listCity   string

	findResult zone.Zone
	findFound  bool
	findErr    error
}

func (r *fakeRepository) Create(_ context.Context, input zone.CreateInput) (zone.Zone, error) {
	r.createCalls = append(r.createCalls, input)
	if r.createErr != nil {
		return zone.Zone{}, r.createErr
	}
	return r.createResult, nil
}

func (r *fakeRepository) Update(_ context.Context, _ zone.UpdateInput) (zone.Zone, error) {
	if r.updateErr != nil {
		return zone.Zone{}, r.updateErr
	}
	return r.updateResult, nil
}

func (r *fakeRepository) SetActive(_ context.Context, _ string, _ bool) (zone.Zone, error) {
	if r.setActiveErr != nil {
		return zone.Zone{}, r.setActiveErr
	}
	return r.setActiveResult, nil
}

func (r *fakeRepository) Get(_ context.Context, _ string) (zone.Zone, error) {
	if r.getErr != nil {
		return zone.Zone{}, r.getErr
	}
	return r.getResult, nil
}

func (r *fakeRepository) List(_ context.Context, city string) ([]zone.Zone, error) {
	r.listCity = city
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listResult, nil
}

func (r *fakeRepository) FindContaining(_ context.Context, _ zone.Coordinates) (zone.Zone, bool, error) {
	if r.findErr != nil {
		return zone.Zone{}, false, r.findErr
	}
	return r.findResult, r.findFound, nil
}

type fakeIDGenerator struct{ id string }

func (g *fakeIDGenerator) NewID() string { return g.id }

func newService(repo *fakeRepository) zone.Service {
	return zone.NewService(repo, &fakeIDGenerator{id: "new-zone-id"})
}

func validBoundary() []zone.Coordinates {
	return []zone.Coordinates{
		{Latitude: 36.19, Longitude: 44.00},
		{Latitude: 36.19, Longitude: 44.02},
		{Latitude: 36.21, Longitude: 44.01},
	}
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnMissingDependencies(t *testing.T) {
	t.Run("nil repository", func(t *testing.T) {
		defer expectPanic(t)
		zone.NewService(nil, &fakeIDGenerator{id: "x"})
	})

	t.Run("nil id generator", func(t *testing.T) {
		defer expectPanic(t)
		zone.NewService(&fakeRepository{}, nil)
	})
}

func expectPanic(t *testing.T) {
	t.Helper()

	if recover() == nil {
		t.Fatal("expected a panic")
	}
}

// --- NewBoundary --------------------------------------------------------

func TestNewBoundary(t *testing.T) {
	t.Run("too few points", func(t *testing.T) {
		_, err := zone.NewBoundary(validBoundary()[:2])
		if !errors.Is(err, zone.ErrBoundaryTooFewPoints) {
			t.Fatalf("got %v, want ErrBoundaryTooFewPoints", err)
		}
	})

	t.Run("invalid latitude in one vertex", func(t *testing.T) {
		points := validBoundary()
		points[1].Latitude = 200
		_, err := zone.NewBoundary(points)
		if !errors.Is(err, zone.ErrInvalidLatitude) {
			t.Fatalf("got %v, want ErrInvalidLatitude", err)
		}
	})

	t.Run("valid triangle", func(t *testing.T) {
		got, err := zone.NewBoundary(validBoundary())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d points, want 3", len(got))
		}
	})
}

// --- CreateZone ---------------------------------------------------------

func TestService_CreateZone_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   zone.CreateZoneInput
		wantErr error
	}{
		{"empty city", zone.CreateZoneInput{City: " ", Name: "Center", Boundary: validBoundary()}, zone.ErrCityRequired},
		{"empty name", zone.CreateZoneInput{City: "Erbil", Name: "", Boundary: validBoundary()}, zone.ErrNameRequired},
		{"bad boundary", zone.CreateZoneInput{City: "Erbil", Name: "Center", Boundary: validBoundary()[:1]}, zone.ErrBoundaryTooFewPoints},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo)

			_, err := svc.CreateZone(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.createCalls) != 0 {
				t.Fatal("expected Create not to be called on validation failure")
			}
		})
	}
}

func TestService_CreateZone_HappyPath(t *testing.T) {
	repo := &fakeRepository{createResult: zone.Zone{ID: "new-zone-id", City: "Erbil", Name: "Center"}}
	svc := newService(repo)

	got, err := svc.CreateZone(context.Background(), zone.CreateZoneInput{
		City:     "  Erbil  ",
		Name:     "  Center  ",
		Boundary: validBoundary(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "new-zone-id" {
		t.Fatalf("got %+v", got)
	}

	if len(repo.createCalls) != 1 {
		t.Fatalf("expected 1 Create call, got %d", len(repo.createCalls))
	}

	call := repo.createCalls[0]
	if call.ID != "new-zone-id" || call.City != "Erbil" || call.Name != "Center" {
		t.Fatalf("unexpected Create input: %+v", call)
	}
}

func TestService_CreateZone_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("connection reset")
	repo := &fakeRepository{createErr: repoErr}
	svc := newService(repo)

	_, err := svc.CreateZone(context.Background(), zone.CreateZoneInput{
		City: "Erbil", Name: "Center", Boundary: validBoundary(),
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

// --- UpdateZone / SetZoneActive --------------------------------------------

func TestService_UpdateZone_RequiresZoneID(t *testing.T) {
	svc := newService(&fakeRepository{})

	_, err := svc.UpdateZone(context.Background(), zone.UpdateZoneInput{
		ZoneID: " ", Name: "Center", Boundary: validBoundary(),
	})
	if !errors.Is(err, zone.ErrZoneIDRequired) {
		t.Fatalf("got %v, want ErrZoneIDRequired", err)
	}
}

func TestService_SetZoneActive_RequiresZoneID(t *testing.T) {
	svc := newService(&fakeRepository{})

	_, err := svc.SetZoneActive(context.Background(), " ", false)
	if !errors.Is(err, zone.ErrZoneIDRequired) {
		t.Fatalf("got %v, want ErrZoneIDRequired", err)
	}
}

func TestService_SetZoneActive_HappyPath(t *testing.T) {
	repo := &fakeRepository{setActiveResult: zone.Zone{ID: "zone-1", Active: false}}
	svc := newService(repo)

	got, err := svc.SetZoneActive(context.Background(), "zone-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Active {
		t.Fatalf("got %+v", got)
	}
}

// --- GetZone / ListZones -----------------------------------------------

func TestService_GetZone_RequiresZoneID(t *testing.T) {
	svc := newService(&fakeRepository{})

	_, err := svc.GetZone(context.Background(), "")
	if !errors.Is(err, zone.ErrZoneIDRequired) {
		t.Fatalf("got %v, want ErrZoneIDRequired", err)
	}
}

func TestService_GetZone_WrapsRepositoryError(t *testing.T) {
	repoErr := zone.ErrZoneNotFound
	repo := &fakeRepository{getErr: repoErr}
	svc := newService(repo)

	_, err := svc.GetZone(context.Background(), "zone-1")
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

func TestService_ListZones_PassesCityFilterThrough(t *testing.T) {
	repo := &fakeRepository{listResult: []zone.Zone{{ID: "zone-1"}}}
	svc := newService(repo)

	got, err := svc.ListZones(context.Background(), "  Erbil  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if repo.listCity != "Erbil" {
		t.Fatalf("got city filter %q, want trimmed \"Erbil\"", repo.listCity)
	}
}

// --- CheckServiceZone -------------------------------------------------

func TestService_CheckServiceZone_ValidatesCoordinates(t *testing.T) {
	svc := newService(&fakeRepository{})

	_, err := svc.CheckServiceZone(context.Background(), 200, 0)
	if !errors.Is(err, zone.ErrInvalidLatitude) {
		t.Fatalf("got %v, want ErrInvalidLatitude", err)
	}
}

func TestService_CheckServiceZone_PointInsideAZone(t *testing.T) {
	repo := &fakeRepository{
		findResult: zone.Zone{ID: "zone-1", City: "Erbil"},
		findFound:  true,
	}
	svc := newService(repo)

	got, err := svc.CheckServiceZone(context.Background(), 36.19, 44.01)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Served || got.ZoneID != "zone-1" || got.City != "Erbil" {
		t.Fatalf("got %+v", got)
	}
}

func TestService_CheckServiceZone_PointOutsideEveryZone(t *testing.T) {
	repo := &fakeRepository{findFound: false}
	svc := newService(repo)

	got, err := svc.CheckServiceZone(context.Background(), 36.19, 44.01)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Served || got.ZoneID != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestService_CheckServiceZone_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("postgres unreachable")
	repo := &fakeRepository{findErr: repoErr}
	svc := newService(repo)

	_, err := svc.CheckServiceZone(context.Background(), 36.19, 44.01)
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}
