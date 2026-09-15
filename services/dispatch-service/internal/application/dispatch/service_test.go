package dispatch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

// --- test doubles -----------------------------------------------------

type fakeTripClient struct {
	trip       dispatch.TripInfo
	getErr     error
	acceptErr  map[string]error // driverID -> error; nil entry means success
	acceptedBy string
}

func (c *fakeTripClient) GetTrip(_ context.Context, _ string) (dispatch.TripInfo, error) {
	if c.getErr != nil {
		return dispatch.TripInfo{}, c.getErr
	}
	return c.trip, nil
}

func (c *fakeTripClient) AcceptTrip(_ context.Context, _ string, driverID string) error {
	if err, ok := c.acceptErr[driverID]; ok {
		return err
	}
	c.acceptedBy = driverID
	return nil
}

type fakeLocationClient struct {
	candidates    []dispatch.NearbyDriver
	err           error
	calledRadius  float64
	calledLimit   int
}

func (c *fakeLocationClient) FindNearbyDrivers(
	_ context.Context,
	_, _, radiusMeters float64,
	limit int,
) ([]dispatch.NearbyDriver, error) {
	c.calledRadius = radiusMeters
	c.calledLimit = limit
	if c.err != nil {
		return nil, c.err
	}
	return c.candidates, nil
}

type fakeDriverClient struct {
	drivers      map[string]dispatch.DriverInfo
	getErrFor    map[string]error
	markBusyErr  error
	markBusyCall string
}

func (c *fakeDriverClient) GetDriver(_ context.Context, driverID string) (dispatch.DriverInfo, error) {
	if err, ok := c.getErrFor[driverID]; ok {
		return dispatch.DriverInfo{}, err
	}
	return c.drivers[driverID], nil
}

func (c *fakeDriverClient) MarkBusy(_ context.Context, driverID string) error {
	c.markBusyCall = driverID
	return c.markBusyErr
}

type fakeWalletClient struct {
	standingFor map[string]dispatch.DriverStanding
	errFor      map[string]error
}

func (c *fakeWalletClient) CheckDriverStanding(_ context.Context, driverID string) (dispatch.DriverStanding, error) {
	if err, ok := c.errFor[driverID]; ok {
		return dispatch.DriverStanding{}, err
	}
	return c.standingFor[driverID], nil
}

// --- fixtures -----------------------------------------------------------

func eligibleDriver(id string) dispatch.DriverInfo {
	return dispatch.DriverInfo{ID: id, Status: "active", AvailabilityStatus: "available"}
}

func goodStanding() dispatch.DriverStanding { return dispatch.DriverStanding{CanTakeTrips: true} }

type harness struct {
	trip     *fakeTripClient
	location *fakeLocationClient
	driver   *fakeDriverClient
	wallet   *fakeWalletClient
}

func newHarness() *harness {
	return &harness{
		trip:     &fakeTripClient{trip: dispatch.TripInfo{ID: "trip-1", Status: "requested", PickupLat: 36.19, PickupLng: 44.01}},
		location: &fakeLocationClient{},
		driver:   &fakeDriverClient{drivers: map[string]dispatch.DriverInfo{}, getErrFor: map[string]error{}},
		wallet:   &fakeWalletClient{standingFor: map[string]dispatch.DriverStanding{}, errFor: map[string]error{}},
	}
}

func (h *harness) service() dispatch.Service {
	return dispatch.NewService(h.trip, h.location, h.driver, h.wallet)
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnMissingDependencies(t *testing.T) {
	h := newHarness()

	cases := []func(){
		func() { dispatch.NewService(nil, h.location, h.driver, h.wallet) },
		func() { dispatch.NewService(h.trip, nil, h.driver, h.wallet) },
		func() { dispatch.NewService(h.trip, h.location, nil, h.wallet) },
		func() { dispatch.NewService(h.trip, h.location, h.driver, nil) },
	}

	for i, fn := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("case %d: expected a panic", i)
				}
			}()
			fn()
		}()
	}
}

// --- DispatchTrip ---------------------------------------------------------

func TestService_DispatchTrip_EmptyTripID(t *testing.T) {
	h := newHarness()
	svc := h.service()

	_, err := svc.DispatchTrip(context.Background(), " ", 0)
	if !errors.Is(err, dispatch.ErrTripIDRequired) {
		t.Fatalf("got %v, want ErrTripIDRequired", err)
	}
}

func TestService_DispatchTrip_WrapsGetTripError(t *testing.T) {
	h := newHarness()
	h.trip.getErr = errors.New("trip service unreachable")
	svc := h.service()

	_, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, h.trip.getErr) {
		t.Fatalf("got %v, want wrapped %v", err, h.trip.getErr)
	}
}

func TestService_DispatchTrip_RejectsNonRequestedTrip(t *testing.T) {
	h := newHarness()
	h.trip.trip.Status = "accepted"
	svc := h.service()

	_, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, dispatch.ErrTripNotDispatchable) {
		t.Fatalf("got %v, want ErrTripNotDispatchable", err)
	}
}

func TestService_DispatchTrip_UsesDefaultRadiusWhenNotPositive(t *testing.T) {
	h := newHarness()
	svc := h.service()

	_, _ = svc.DispatchTrip(context.Background(), "trip-1", 0)
	if h.location.calledRadius != dispatch.DefaultSearchRadiusMeters {
		t.Fatalf("got radius %v, want default %v", h.location.calledRadius, dispatch.DefaultSearchRadiusMeters)
	}

	_, _ = svc.DispatchTrip(context.Background(), "trip-1", 2000)
	if h.location.calledRadius != 2000 {
		t.Fatalf("got radius %v, want 2000", h.location.calledRadius)
	}
}

func TestService_DispatchTrip_WrapsFindNearbyDriversError(t *testing.T) {
	h := newHarness()
	h.location.err = errors.New("location service unreachable")
	svc := h.service()

	_, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, h.location.err) {
		t.Fatalf("got %v, want wrapped %v", err, h.location.err)
	}
}

func TestService_DispatchTrip_NoDriversNearby(t *testing.T) {
	h := newHarness()
	svc := h.service()

	_, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, dispatch.ErrNoDriversNearby) {
		t.Fatalf("got %v, want ErrNoDriversNearby", err)
	}
}

func TestService_DispatchTrip_HappyPath_AssignsNearestEligibleDriver(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1", DistanceMeters: 500}}
	h.driver.drivers["driver-1"] = eligibleDriver("driver-1")
	h.wallet.standingFor["driver-1"] = goodStanding()
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DriverID != "driver-1" || result.DistanceMeters != 500 {
		t.Fatalf("got %+v", result)
	}

	if h.trip.acceptedBy != "driver-1" {
		t.Fatalf("expected AcceptTrip to be called for driver-1, got %q", h.trip.acceptedBy)
	}

	if h.driver.markBusyCall != "driver-1" {
		t.Fatalf("expected MarkBusy to be called for driver-1, got %q", h.driver.markBusyCall)
	}
}

func TestService_DispatchTrip_SucceedsEvenIfMarkBusyFails(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1"}}
	h.driver.drivers["driver-1"] = eligibleDriver("driver-1")
	h.driver.markBusyErr = errors.New("driver service unreachable")
	h.wallet.standingFor["driver-1"] = goodStanding()
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("expected success despite MarkBusy failure, got: %v", err)
	}
	if result.DriverID != "driver-1" {
		t.Fatalf("got %+v", result)
	}
}

func TestService_DispatchTrip_SkipsCandidateOnGetDriverError(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{
		{DriverID: "driver-broken"},
		{DriverID: "driver-2"},
	}
	h.driver.getErrFor["driver-broken"] = errors.New("not found")
	h.driver.drivers["driver-2"] = eligibleDriver("driver-2")
	h.wallet.standingFor["driver-2"] = goodStanding()
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DriverID != "driver-2" {
		t.Fatalf("expected fallback to driver-2, got %+v", result)
	}
}

func TestService_DispatchTrip_SkipsInactiveOrUnavailableDrivers(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{
		{DriverID: "driver-inactive"},
		{DriverID: "driver-busy"},
		{DriverID: "driver-2"},
	}
	h.driver.drivers["driver-inactive"] = dispatch.DriverInfo{ID: "driver-inactive", Status: "suspended", AvailabilityStatus: "available"}
	h.driver.drivers["driver-busy"] = dispatch.DriverInfo{ID: "driver-busy", Status: "active", AvailabilityStatus: "busy"}
	h.driver.drivers["driver-2"] = eligibleDriver("driver-2")
	h.wallet.standingFor["driver-2"] = goodStanding()
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DriverID != "driver-2" {
		t.Fatalf("expected fallback to driver-2, got %+v", result)
	}
}

func TestService_DispatchTrip_SkipsCandidateWithWalletCheckError(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1"}, {DriverID: "driver-2"}}
	h.driver.drivers["driver-1"] = eligibleDriver("driver-1")
	h.driver.drivers["driver-2"] = eligibleDriver("driver-2")
	h.wallet.errFor["driver-1"] = errors.New("wallet service unreachable")
	h.wallet.standingFor["driver-2"] = goodStanding()
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DriverID != "driver-2" {
		t.Fatalf("expected fallback to driver-2, got %+v", result)
	}
}

func TestService_DispatchTrip_SkipsSuspendedDriver(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1"}, {DriverID: "driver-2"}}
	h.driver.drivers["driver-1"] = eligibleDriver("driver-1")
	h.driver.drivers["driver-2"] = eligibleDriver("driver-2")
	h.wallet.standingFor["driver-1"] = dispatch.DriverStanding{CanTakeTrips: false, Suspended: true}
	h.wallet.standingFor["driver-2"] = goodStanding()
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DriverID != "driver-2" {
		t.Fatalf("expected fallback to driver-2, got %+v", result)
	}
}

func TestService_DispatchTrip_SkipsCandidateThatLosesAcceptRace(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1"}, {DriverID: "driver-2"}}
	h.driver.drivers["driver-1"] = eligibleDriver("driver-1")
	h.driver.drivers["driver-2"] = eligibleDriver("driver-2")
	h.wallet.standingFor["driver-1"] = goodStanding()
	h.wallet.standingFor["driver-2"] = goodStanding()
	h.trip.acceptErr = map[string]error{"driver-1": errors.New("trip already accepted")}
	svc := h.service()

	result, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DriverID != "driver-2" {
		t.Fatalf("expected fallback to driver-2, got %+v", result)
	}
}

func TestService_DispatchTrip_NoDriversAvailableWhenAllCandidatesFailEligibility(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1"}}
	h.driver.drivers["driver-1"] = dispatch.DriverInfo{ID: "driver-1", Status: "suspended", AvailabilityStatus: "available"}
	svc := h.service()

	_, err := svc.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, dispatch.ErrNoDriversAvailable) {
		t.Fatalf("got %v, want ErrNoDriversAvailable", err)
	}
}
