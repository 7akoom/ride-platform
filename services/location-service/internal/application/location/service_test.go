package location_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	updateResult time.Time
	updateErr    error
	updateCalls  []location.UpdateInput

	getResult location.Location
	getErr    error

	findNearbyResult []location.NearbyEntity
	findNearbyErr    error
	findNearbyCalls  []location.NearbySearchInput
}

func (r *fakeRepository) Update(_ context.Context, input location.UpdateInput) (time.Time, error) {
	r.updateCalls = append(r.updateCalls, input)
	if r.updateErr != nil {
		return time.Time{}, r.updateErr
	}
	return r.updateResult, nil
}

func (r *fakeRepository) Get(_ context.Context, _ location.EntityType, _ string) (location.Location, error) {
	if r.getErr != nil {
		return location.Location{}, r.getErr
	}
	return r.getResult, nil
}

func (r *fakeRepository) FindNearby(_ context.Context, input location.NearbySearchInput) ([]location.NearbyEntity, error) {
	r.findNearbyCalls = append(r.findNearbyCalls, input)
	if r.findNearbyErr != nil {
		return nil, r.findNearbyErr
	}
	return r.findNearbyResult, nil
}

func newService(repo *fakeRepository) location.Service {
	return location.NewService(repo)
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnNilRepository(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	location.NewService(nil)
}

// --- NewCoordinates ---------------------------------------------------------

func TestNewCoordinates(t *testing.T) {
	cases := []struct {
		name    string
		lat     float64
		lng     float64
		wantErr error
	}{
		{"valid", 36.19, 44.01, nil},
		{"lat too high", 90.1, 0, location.ErrInvalidLatitude},
		{"lat too low", -90.1, 0, location.ErrInvalidLatitude},
		{"lng too high", 0, 180.1, location.ErrInvalidLongitude},
		{"lng too low", 0, -180.1, location.ErrInvalidLongitude},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := location.NewCoordinates(tc.lat, tc.lng)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// --- UpdateLocation -----------------------------------------------------

func TestService_UpdateLocation_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   location.UpdateLocationInput
		wantErr error
	}{
		{"invalid entity type", location.UpdateLocationInput{EntityType: "vehicle", EntityID: "id-1"}, location.ErrInvalidEntityType},
		{"empty entity id", location.UpdateLocationInput{EntityType: location.EntityDriver, EntityID: " "}, location.ErrEntityIDRequired},
		{"invalid latitude", location.UpdateLocationInput{EntityType: location.EntityDriver, EntityID: "id-1", Latitude: 200}, location.ErrInvalidLatitude},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo)

			_, err := svc.UpdateLocation(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.updateCalls) != 0 {
				t.Fatal("expected Update not to be called on validation failure")
			}
		})
	}
}

func TestService_UpdateLocation_AlwaysUsesTheDefaultTTL(t *testing.T) {
	repo := &fakeRepository{}
	svc := newService(repo)

	_, err := svc.UpdateLocation(context.Background(), location.UpdateLocationInput{
		EntityType: location.EntityDriver,
		EntityID:   "driver-1",
		Latitude:   36.19,
		Longitude:  44.01,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.updateCalls) != 1 || repo.updateCalls[0].TTL != location.DefaultTTL {
		t.Fatalf("unexpected Update call: %+v", repo.updateCalls)
	}
}

func TestService_UpdateLocation_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("valkey unreachable")
	repo := &fakeRepository{updateErr: repoErr}
	svc := newService(repo)

	_, err := svc.UpdateLocation(context.Background(), location.UpdateLocationInput{
		EntityType: location.EntityDriver,
		EntityID:   "driver-1",
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

// --- GetLocation --------------------------------------------------------

func TestService_GetLocation_ValidationErrors(t *testing.T) {
	svc := newService(&fakeRepository{})

	if _, err := svc.GetLocation(context.Background(), "vehicle", "id-1"); !errors.Is(err, location.ErrInvalidEntityType) {
		t.Fatalf("got %v, want ErrInvalidEntityType", err)
	}
	if _, err := svc.GetLocation(context.Background(), location.EntityRider, " "); !errors.Is(err, location.ErrEntityIDRequired) {
		t.Fatalf("got %v, want ErrEntityIDRequired", err)
	}
}

func TestService_GetLocation_WrapsRepositoryError(t *testing.T) {
	repoErr := location.ErrLocationNotFound
	repo := &fakeRepository{getErr: repoErr}
	svc := newService(repo)

	_, err := svc.GetLocation(context.Background(), location.EntityDriver, "driver-1")
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

// --- FindNearby -----------------------------------------------------------

func TestService_FindNearby_ValidationErrors(t *testing.T) {
	valid := location.FindNearbyInput{
		EntityType:   location.EntityDriver,
		Latitude:     36.19,
		Longitude:    44.01,
		RadiusMeters: 3000,
	}

	cases := []struct {
		name    string
		mutate  func(location.FindNearbyInput) location.FindNearbyInput
		wantErr error
	}{
		{"invalid entity type", func(i location.FindNearbyInput) location.FindNearbyInput { i.EntityType = "vehicle"; return i }, location.ErrInvalidEntityType},
		{"invalid latitude", func(i location.FindNearbyInput) location.FindNearbyInput { i.Latitude = 200; return i }, location.ErrInvalidLatitude},
		{"zero radius", func(i location.FindNearbyInput) location.FindNearbyInput { i.RadiusMeters = 0; return i }, location.ErrInvalidRadius},
		{"negative radius", func(i location.FindNearbyInput) location.FindNearbyInput { i.RadiusMeters = -1; return i }, location.ErrInvalidRadius},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo)

			_, err := svc.FindNearby(context.Background(), tc.mutate(valid))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestService_FindNearby_ClampsLimit(t *testing.T) {
	cases := []struct {
		name  string
		given int
		want  int
	}{
		{"zero uses default", 0, 20},
		{"negative uses default", -5, 20},
		{"within range unchanged", 50, 50},
		{"above max is capped", 500, 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo)

			_, err := svc.FindNearby(context.Background(), location.FindNearbyInput{
				EntityType:   location.EntityDriver,
				Latitude:     36.19,
				Longitude:    44.01,
				RadiusMeters: 3000,
				Limit:        tc.given,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if repo.findNearbyCalls[0].Limit != tc.want {
				t.Fatalf("got limit %d, want %d", repo.findNearbyCalls[0].Limit, tc.want)
			}
		})
	}
}

func TestService_FindNearby_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("valkey unreachable")
	repo := &fakeRepository{findNearbyErr: repoErr}
	svc := newService(repo)

	_, err := svc.FindNearby(context.Background(), location.FindNearbyInput{
		EntityType:   location.EntityDriver,
		Latitude:     36.19,
		Longitude:    44.01,
		RadiusMeters: 3000,
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}
