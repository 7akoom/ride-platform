package rider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	findByIdentityIDResult rider.Rider
	findByIdentityIDErr    error

	createResult rider.Rider
	createErr    error
	createCalls  []rider.CreateInput

	findByIDResult rider.Rider
	findByIDErr    error

	updateProfileResult rider.Rider
	updateProfileErr    error
	updateProfileCalls  []rider.UpdateProfileInput
}

func (r *fakeRepository) Create(
	_ context.Context,
	input rider.CreateInput,
) (rider.Rider, error) {
	r.createCalls = append(r.createCalls, input)

	if r.createErr != nil {
		return rider.Rider{}, r.createErr
	}

	return r.createResult, nil
}

func (r *fakeRepository) FindByID(
	_ context.Context,
	_ string,
) (rider.Rider, error) {
	if r.findByIDErr != nil {
		return rider.Rider{}, r.findByIDErr
	}

	return r.findByIDResult, nil
}

func (r *fakeRepository) FindByIdentityID(
	_ context.Context,
	_ string,
) (rider.Rider, error) {
	if r.findByIdentityIDErr != nil {
		return rider.Rider{}, r.findByIdentityIDErr
	}

	return r.findByIdentityIDResult, nil
}

func (r *fakeRepository) UpdateProfile(
	_ context.Context,
	input rider.UpdateProfileInput,
) (rider.Rider, error) {
	r.updateProfileCalls = append(r.updateProfileCalls, input)

	if r.updateProfileErr != nil {
		return rider.Rider{}, r.updateProfileErr
	}

	return r.updateProfileResult, nil
}

type fakeIDGenerator struct{ id string }

func (g *fakeIDGenerator) NewID() string { return g.id }

func newService(repo *fakeRepository, id string) rider.Service {
	return rider.NewService(repo, &fakeIDGenerator{id: id})
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnMissingDependencies(t *testing.T) {
	t.Run("nil repository", func(t *testing.T) {
		defer expectPanic(t)
		rider.NewService(nil, &fakeIDGenerator{id: "x"})
	})

	t.Run("nil id generator", func(t *testing.T) {
		defer expectPanic(t)
		rider.NewService(&fakeRepository{}, nil)
	})
}

func expectPanic(t *testing.T) {
	t.Helper()

	if recover() == nil {
		t.Fatal("expected a panic")
	}
}

// --- CreateRider --------------------------------------------------------

func TestService_CreateRider_ValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		input rider.CreateRiderInput
		want  error
	}{
		{"empty identity id", rider.CreateRiderInput{IdentityID: "  ", DisplayName: "Ali"}, rider.ErrIdentityIDRequired},
		{"empty display name", rider.CreateRiderInput{IdentityID: "id-1", DisplayName: "  "}, rider.ErrDisplayNameRequired},
		{"display name too long", rider.CreateRiderInput{IdentityID: "id-1", DisplayName: strings.Repeat("a", 121)}, rider.ErrDisplayNameTooLong},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{findByIdentityIDErr: rider.ErrRiderNotFound}
			svc := newService(repo, "new-id")

			_, err := svc.CreateRider(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got error %v, want %v", err, tc.want)
			}

			if len(repo.createCalls) != 0 {
				t.Fatal("expected Create not to be called on validation failure")
			}
		})
	}
}

func TestService_CreateRider_AlreadyExistsForIdentity(t *testing.T) {
	repo := &fakeRepository{
		findByIdentityIDResult: rider.Rider{ID: "existing-rider"},
		findByIdentityIDErr:    nil,
	}
	svc := newService(repo, "new-id")

	_, err := svc.CreateRider(context.Background(), rider.CreateRiderInput{
		IdentityID:  "id-1",
		DisplayName: "Ali",
	})
	if !errors.Is(err, rider.ErrRiderAlreadyExists) {
		t.Fatalf("got %v, want ErrRiderAlreadyExists", err)
	}

	if len(repo.createCalls) != 0 {
		t.Fatal("expected Create not to be called when a rider already exists")
	}
}

func TestService_CreateRider_WrapsUnexpectedLookupError(t *testing.T) {
	lookupErr := errors.New("connection reset")
	repo := &fakeRepository{findByIdentityIDErr: lookupErr}
	svc := newService(repo, "new-id")

	_, err := svc.CreateRider(context.Background(), rider.CreateRiderInput{
		IdentityID:  "id-1",
		DisplayName: "Ali",
	})
	if !errors.Is(err, lookupErr) {
		t.Fatalf("got %v, want wrapped %v", err, lookupErr)
	}
}

func TestService_CreateRider_WrapsRepositoryCreateError(t *testing.T) {
	createErr := errors.New("unique violation")
	repo := &fakeRepository{
		findByIdentityIDErr: rider.ErrRiderNotFound,
		createErr:           createErr,
	}
	svc := newService(repo, "new-id")

	_, err := svc.CreateRider(context.Background(), rider.CreateRiderInput{
		IdentityID:  "id-1",
		DisplayName: "Ali",
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("got %v, want wrapped %v", err, createErr)
	}
}

func TestService_CreateRider_HappyPath(t *testing.T) {
	repo := &fakeRepository{
		findByIdentityIDErr: rider.ErrRiderNotFound,
		createResult:        rider.Rider{ID: "new-id", IdentityID: "id-1", DisplayName: "Ali"},
	}
	svc := newService(repo, "new-id")

	got, err := svc.CreateRider(context.Background(), rider.CreateRiderInput{
		IdentityID:  "id-1",
		DisplayName: "  Ali  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "new-id" {
		t.Fatalf("got rider %+v", got)
	}

	if len(repo.createCalls) != 1 {
		t.Fatalf("expected exactly 1 Create call, got %d", len(repo.createCalls))
	}

	call := repo.createCalls[0]
	if call.ID != "new-id" || call.IdentityID != "id-1" || call.DisplayName != "Ali" {
		t.Fatalf("unexpected Create input: %+v", call)
	}
}

// --- GetRider / GetRiderByIdentityID -------------------------------------

func TestService_GetRider_EmptyID(t *testing.T) {
	svc := newService(&fakeRepository{}, "id")

	_, err := svc.GetRider(context.Background(), "   ")
	if !errors.Is(err, rider.ErrRiderIDRequired) {
		t.Fatalf("got %v, want ErrRiderIDRequired", err)
	}
}

func TestService_GetRider_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("not found")
	repo := &fakeRepository{findByIDErr: repoErr}
	svc := newService(repo, "id")

	_, err := svc.GetRider(context.Background(), "rider-1")
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

func TestService_GetRider_HappyPath(t *testing.T) {
	repo := &fakeRepository{findByIDResult: rider.Rider{ID: "rider-1"}}
	svc := newService(repo, "id")

	got, err := svc.GetRider(context.Background(), "rider-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "rider-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestService_GetRiderByIdentityID_EmptyID(t *testing.T) {
	svc := newService(&fakeRepository{}, "id")

	_, err := svc.GetRiderByIdentityID(context.Background(), "")
	if !errors.Is(err, rider.ErrIdentityIDRequired) {
		t.Fatalf("got %v, want ErrIdentityIDRequired", err)
	}
}

func TestService_GetRiderByIdentityID_HappyPath(t *testing.T) {
	repo := &fakeRepository{findByIdentityIDResult: rider.Rider{ID: "rider-1", IdentityID: "identity-1"}}
	svc := newService(repo, "id")

	got, err := svc.GetRiderByIdentityID(context.Background(), "identity-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.IdentityID != "identity-1" {
		t.Fatalf("got %+v", got)
	}
}

// --- UpdateRiderProfile ---------------------------------------------------

func TestService_UpdateRiderProfile_ValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		input rider.UpdateRiderProfileInput
		want  error
	}{
		{"empty rider id", rider.UpdateRiderProfileInput{RiderID: " ", DisplayName: "Ali"}, rider.ErrRiderIDRequired},
		{"empty display name", rider.UpdateRiderProfileInput{RiderID: "rider-1", DisplayName: ""}, rider.ErrDisplayNameRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo, "id")

			_, err := svc.UpdateRiderProfile(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(repo.updateProfileCalls) != 0 {
				t.Fatal("expected UpdateProfile not to be called on validation failure")
			}
		})
	}
}

func TestService_UpdateRiderProfile_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("row locked")
	repo := &fakeRepository{updateProfileErr: repoErr}
	svc := newService(repo, "id")

	_, err := svc.UpdateRiderProfile(context.Background(), rider.UpdateRiderProfileInput{
		RiderID:     "rider-1",
		DisplayName: "New Name",
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

func TestService_UpdateRiderProfile_HappyPath(t *testing.T) {
	repo := &fakeRepository{updateProfileResult: rider.Rider{ID: "rider-1", DisplayName: "New Name"}}
	svc := newService(repo, "id")

	got, err := svc.UpdateRiderProfile(context.Background(), rider.UpdateRiderProfileInput{
		RiderID:     "rider-1",
		DisplayName: "  New Name  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.DisplayName != "New Name" {
		t.Fatalf("got %+v", got)
	}

	if len(repo.updateProfileCalls) != 1 || repo.updateProfileCalls[0].DisplayName != "New Name" {
		t.Fatalf("unexpected UpdateProfile call: %+v", repo.updateProfileCalls)
	}
}

// --- DisplayName value object ---------------------------------------------

func TestNewDisplayName(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"valid", "Ali Hassan", nil},
		{"trims whitespace", "  Ali  ", nil},
		{"empty", "   ", rider.ErrDisplayNameRequired},
		{"too long", strings.Repeat("a", 121), rider.ErrDisplayNameTooLong},
		{"exactly at limit", strings.Repeat("a", 120), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rider.NewDisplayName(tc.raw)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got error %v, want %v", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.String() != strings.TrimSpace(tc.raw) {
				t.Fatalf("got %q, want %q", got.String(), strings.TrimSpace(tc.raw))
			}
		})
	}
}
