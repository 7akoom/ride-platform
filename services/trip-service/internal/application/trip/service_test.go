package trip_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	createResult trip.Trip
	createErr    error
	createCalls  []trip.CreateInput

	findByIDResult trip.Trip
	findByIDErr    error

	findActiveByRiderIDResult trip.Trip
	findActiveByRiderIDErr    error

	findActiveByDriverIDResult trip.Trip
	findActiveByDriverIDErr    error

	acceptResult trip.Trip
	acceptErr    error
	acceptCalls  []string // driverID passed

	startResult trip.Trip
	startErr    error

	completeResult trip.Trip
	completeErr    error

	cancelResult trip.Trip
	cancelErr    error
	cancelReason string
}

func (r *fakeRepository) Create(_ context.Context, input trip.CreateInput) (trip.Trip, error) {
	r.createCalls = append(r.createCalls, input)
	if r.createErr != nil {
		return trip.Trip{}, r.createErr
	}
	return r.createResult, nil
}

func (r *fakeRepository) FindByID(_ context.Context, _ string) (trip.Trip, error) {
	if r.findByIDErr != nil {
		return trip.Trip{}, r.findByIDErr
	}
	return r.findByIDResult, nil
}

func (r *fakeRepository) FindActiveByRiderID(_ context.Context, _ string) (trip.Trip, error) {
	if r.findActiveByRiderIDErr != nil {
		return trip.Trip{}, r.findActiveByRiderIDErr
	}
	return r.findActiveByRiderIDResult, nil
}

func (r *fakeRepository) FindActiveByDriverID(_ context.Context, _ string) (trip.Trip, error) {
	if r.findActiveByDriverIDErr != nil {
		return trip.Trip{}, r.findActiveByDriverIDErr
	}
	return r.findActiveByDriverIDResult, nil
}

func (r *fakeRepository) Accept(_ context.Context, _ string, driverID string) (trip.Trip, error) {
	r.acceptCalls = append(r.acceptCalls, driverID)
	if r.acceptErr != nil {
		return trip.Trip{}, r.acceptErr
	}
	return r.acceptResult, nil
}

func (r *fakeRepository) Start(_ context.Context, _ string) (trip.Trip, error) {
	if r.startErr != nil {
		return trip.Trip{}, r.startErr
	}
	return r.startResult, nil
}

func (r *fakeRepository) Complete(_ context.Context, _ string) (trip.Trip, error) {
	if r.completeErr != nil {
		return trip.Trip{}, r.completeErr
	}
	return r.completeResult, nil
}

func (r *fakeRepository) Cancel(_ context.Context, _ string, reason string) (trip.Trip, error) {
	r.cancelReason = reason
	if r.cancelErr != nil {
		return trip.Trip{}, r.cancelErr
	}
	return r.cancelResult, nil
}

type fakeIDGenerator struct{ id string }

func (g *fakeIDGenerator) NewID() string { return g.id }

func newService(repo *fakeRepository) trip.Service {
	return trip.NewService(repo, &fakeIDGenerator{id: "new-trip-id"})
}

func validRequestInput() trip.RequestTripInput {
	return trip.RequestTripInput{
		RiderID:    "rider-1",
		PickupLat:  36.19,
		PickupLng:  44.01,
		DropoffLat: 36.20,
		DropoffLng: 44.02,
	}
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnMissingDependencies(t *testing.T) {
	t.Run("nil repository", func(t *testing.T) {
		defer expectPanic(t)
		trip.NewService(nil, &fakeIDGenerator{id: "x"})
	})

	t.Run("nil id generator", func(t *testing.T) {
		defer expectPanic(t)
		trip.NewService(&fakeRepository{}, nil)
	})
}

func expectPanic(t *testing.T) {
	t.Helper()

	if recover() == nil {
		t.Fatal("expected a panic")
	}
}

// --- Status.CanTransitionTo (the state machine) --------------------------

func TestStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from trip.Status
		to   trip.Status
		want bool
	}{
		{trip.StatusRequested, trip.StatusAccepted, true},
		{trip.StatusRequested, trip.StatusCancelled, true},
		{trip.StatusRequested, trip.StatusInProgress, false},
		{trip.StatusRequested, trip.StatusCompleted, false},

		{trip.StatusAccepted, trip.StatusInProgress, true},
		{trip.StatusAccepted, trip.StatusCancelled, true},
		{trip.StatusAccepted, trip.StatusRequested, false},
		{trip.StatusAccepted, trip.StatusCompleted, false},

		{trip.StatusInProgress, trip.StatusCompleted, true},
		{trip.StatusInProgress, trip.StatusCancelled, true},
		{trip.StatusInProgress, trip.StatusAccepted, false},

		{trip.StatusCompleted, trip.StatusAccepted, false},
		{trip.StatusCompleted, trip.StatusCancelled, false},
		{trip.StatusCancelled, trip.StatusAccepted, false},
		{trip.StatusCancelled, trip.StatusRequested, false},
	}

	for _, tc := range cases {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			got := tc.from.CanTransitionTo(tc.to)
			if got != tc.want {
				t.Fatalf("%s -> %s = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

// --- Coordinates ------------------------------------------------------------

func TestNewCoordinates(t *testing.T) {
	cases := []struct {
		name    string
		lat     float64
		lng     float64
		wantErr error
	}{
		{"valid", 36.19, 44.01, nil},
		{"boundary lat 90", 90, 0, nil},
		{"boundary lat -90", -90, 0, nil},
		{"boundary lng 180", 0, 180, nil},
		{"boundary lng -180", 0, -180, nil},
		{"lat too high", 90.1, 0, trip.ErrInvalidLatitude},
		{"lat too low", -90.1, 0, trip.ErrInvalidLatitude},
		{"lng too high", 0, 180.1, trip.ErrInvalidLongitude},
		{"lng too low", 0, -180.1, trip.ErrInvalidLongitude},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := trip.NewCoordinates(tc.lat, tc.lng)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// --- RequestTrip ------------------------------------------------------------

func TestService_RequestTrip_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(trip.RequestTripInput) trip.RequestTripInput
		wantErr error
	}{
		{"empty rider id", func(i trip.RequestTripInput) trip.RequestTripInput {
			i.RiderID = " "
			return i
		}, trip.ErrRiderIDRequired},
		{"invalid pickup lat", func(i trip.RequestTripInput) trip.RequestTripInput {
			i.PickupLat = 200
			return i
		}, trip.ErrInvalidLatitude},
		{"invalid dropoff lng", func(i trip.RequestTripInput) trip.RequestTripInput {
			i.DropoffLng = -200
			return i
		}, trip.ErrInvalidLongitude},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
			svc := newService(repo)

			_, err := svc.RequestTrip(context.Background(), tc.mutate(validRequestInput()))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.createCalls) != 0 {
				t.Fatal("expected Create not to be called on validation failure")
			}
		})
	}
}

func TestService_RequestTrip_RejectsWhenRiderHasActiveTrip(t *testing.T) {
	repo := &fakeRepository{findActiveByRiderIDResult: trip.Trip{ID: "active-trip"}}
	svc := newService(repo)

	_, err := svc.RequestTrip(context.Background(), validRequestInput())
	if !errors.Is(err, trip.ErrRiderHasActiveTrip) {
		t.Fatalf("got %v, want ErrRiderHasActiveTrip", err)
	}

	if len(repo.createCalls) != 0 {
		t.Fatal("expected Create not to be called")
	}
}

func TestService_RequestTrip_WrapsUnexpectedLookupError(t *testing.T) {
	lookupErr := errors.New("connection reset")
	repo := &fakeRepository{findActiveByRiderIDErr: lookupErr}
	svc := newService(repo)

	_, err := svc.RequestTrip(context.Background(), validRequestInput())
	if !errors.Is(err, lookupErr) {
		t.Fatalf("got %v, want wrapped %v", err, lookupErr)
	}
}

func TestService_RequestTrip_HappyPath(t *testing.T) {
	repo := &fakeRepository{
		findActiveByRiderIDErr: trip.ErrTripNotFound,
		createResult:           trip.Trip{ID: "new-trip-id", RiderID: "rider-1"},
	}
	svc := newService(repo)

	got, err := svc.RequestTrip(context.Background(), validRequestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "new-trip-id" {
		t.Fatalf("got %+v", got)
	}

	if len(repo.createCalls) != 1 || repo.createCalls[0].RiderID != "rider-1" {
		t.Fatalf("unexpected Create calls: %+v", repo.createCalls)
	}
}

// --- AcceptTrip ---------------------------------------------------------

func TestService_AcceptTrip_ValidationErrors(t *testing.T) {
	cases := []struct {
		name     string
		tripID   string
		driverID string
		wantErr  error
	}{
		{"empty trip id", " ", "driver-1", trip.ErrTripIDRequired},
		{"empty driver id", "trip-1", " ", trip.ErrDriverIDRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo)

			_, err := svc.AcceptTrip(context.Background(), tc.tripID, tc.driverID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestService_AcceptTrip_RejectsWhenDriverHasActiveTrip(t *testing.T) {
	repo := &fakeRepository{findActiveByDriverIDResult: trip.Trip{ID: "other-trip"}}
	svc := newService(repo)

	_, err := svc.AcceptTrip(context.Background(), "trip-1", "driver-1")
	if !errors.Is(err, trip.ErrDriverHasActiveTrip) {
		t.Fatalf("got %v, want ErrDriverHasActiveTrip", err)
	}

	if len(repo.acceptCalls) != 0 {
		t.Fatal("expected Accept not to be called")
	}
}

func TestService_AcceptTrip_WrapsRepositoryAcceptError(t *testing.T) {
	acceptErr := errors.New("trip already accepted")
	repo := &fakeRepository{
		findActiveByDriverIDErr: trip.ErrTripNotFound,
		acceptErr:               acceptErr,
	}
	svc := newService(repo)

	_, err := svc.AcceptTrip(context.Background(), "trip-1", "driver-1")
	if !errors.Is(err, acceptErr) {
		t.Fatalf("got %v, want wrapped %v", err, acceptErr)
	}
}

func TestService_AcceptTrip_HappyPath(t *testing.T) {
	repo := &fakeRepository{
		findActiveByDriverIDErr: trip.ErrTripNotFound,
		acceptResult:            trip.Trip{ID: "trip-1", DriverID: "driver-1", Status: trip.StatusAccepted},
	}
	svc := newService(repo)

	got, err := svc.AcceptTrip(context.Background(), "trip-1", "driver-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Status != trip.StatusAccepted {
		t.Fatalf("got %+v", got)
	}

	if len(repo.acceptCalls) != 1 || repo.acceptCalls[0] != "driver-1" {
		t.Fatalf("unexpected Accept calls: %+v", repo.acceptCalls)
	}
}

// --- StartTrip / CompleteTrip / CancelTrip / GetTrip ------------------------

func TestService_LifecycleMethods_RequireTripID(t *testing.T) {
	svc := newService(&fakeRepository{})

	if _, err := svc.StartTrip(context.Background(), " "); !errors.Is(err, trip.ErrTripIDRequired) {
		t.Fatalf("StartTrip: got %v, want ErrTripIDRequired", err)
	}
	if _, err := svc.CompleteTrip(context.Background(), ""); !errors.Is(err, trip.ErrTripIDRequired) {
		t.Fatalf("CompleteTrip: got %v, want ErrTripIDRequired", err)
	}
	if _, err := svc.CancelTrip(context.Background(), "", "reason"); !errors.Is(err, trip.ErrTripIDRequired) {
		t.Fatalf("CancelTrip: got %v, want ErrTripIDRequired", err)
	}
	if _, err := svc.GetTrip(context.Background(), ""); !errors.Is(err, trip.ErrTripIDRequired) {
		t.Fatalf("GetTrip: got %v, want ErrTripIDRequired", err)
	}
}

func TestService_StartTrip_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("invalid transition")
	repo := &fakeRepository{startErr: repoErr}
	svc := newService(repo)

	_, err := svc.StartTrip(context.Background(), "trip-1")
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}

func TestService_CompleteTrip_HappyPath(t *testing.T) {
	repo := &fakeRepository{completeResult: trip.Trip{ID: "trip-1", Status: trip.StatusCompleted}}
	svc := newService(repo)

	got, err := svc.CompleteTrip(context.Background(), "trip-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != trip.StatusCompleted {
		t.Fatalf("got %+v", got)
	}
}

func TestService_CancelTrip_PassesTrimmedReason(t *testing.T) {
	repo := &fakeRepository{cancelResult: trip.Trip{ID: "trip-1", Status: trip.StatusCancelled}}
	svc := newService(repo)

	_, err := svc.CancelTrip(context.Background(), "trip-1", "  rider changed mind  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.cancelReason != "rider changed mind" {
		t.Fatalf("got reason %q", repo.cancelReason)
	}
}

func TestService_GetTrip_HappyPath(t *testing.T) {
	repo := &fakeRepository{findByIDResult: trip.Trip{ID: "trip-1"}}
	svc := newService(repo)

	got, err := svc.GetTrip(context.Background(), "trip-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "trip-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestService_GetTrip_WrapsRepositoryError(t *testing.T) {
	repoErr := errors.New("not found")
	repo := &fakeRepository{findByIDErr: repoErr}
	svc := newService(repo)

	_, err := svc.GetTrip(context.Background(), "trip-1")
	if !errors.Is(err, repoErr) {
		t.Fatalf("got %v, want wrapped %v", err, repoErr)
	}
}
