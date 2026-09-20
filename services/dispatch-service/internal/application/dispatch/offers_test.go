package dispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// offersTrips is a trip client that can offer. offerResults maps a driver to what
// OfferTrip answers for them; a driver not in it is offered successfully.
type offersTrips struct {
	info          TripInfo
	acceptErr     error
	offerResults  map[string]error
	offered       []string
	offeredTTL    []time.Duration
	acceptedCalls []string
}

func (f *offersTrips) GetTrip(context.Context, string) (TripInfo, error) { return f.info, nil }

func (f *offersTrips) AcceptTrip(_ context.Context, _ string, driverID string) error {
	f.acceptedCalls = append(f.acceptedCalls, driverID)

	return f.acceptErr
}

func (f *offersTrips) OfferTrip(_ context.Context, _ string, driverID string, ttl time.Duration) error {
	f.offered = append(f.offered, driverID)
	f.offeredTTL = append(f.offeredTTL, ttl)

	return f.offerResults[driverID]
}

// offersTripsWithoutOffers cannot offer.
type offersTripsWithoutOffers struct{}

func (offersTripsWithoutOffers) GetTrip(context.Context, string) (TripInfo, error) {
	return TripInfo{}, nil
}
func (offersTripsWithoutOffers) AcceptTrip(context.Context, string, string) error { return nil }

type offersLocation struct{ drivers []string }

func (f offersLocation) FindNearbyDrivers(context.Context, float64, float64, float64, int) ([]NearbyDriver, error) {
	found := make([]NearbyDriver, 0, len(f.drivers))
	for i, id := range f.drivers {
		found = append(found, NearbyDriver{DriverID: id, DistanceMeters: float64(100 * (i + 1))})
	}

	return found, nil
}

type offersDrivers struct {
	info map[string]DriverInfo
	busy []string
}

func (f *offersDrivers) GetDriver(_ context.Context, id string) (DriverInfo, error) {
	info, ok := f.info[id]
	if !ok {
		return DriverInfo{}, fmt.Errorf("no such driver %s", id)
	}

	return info, nil
}

func (f *offersDrivers) MarkBusy(_ context.Context, id string) error {
	f.busy = append(f.busy, id)

	return nil
}

type offersWallet struct {
	standing map[string]DriverStanding
	err      map[string]error
}

func (f offersWallet) CheckDriverStanding(_ context.Context, id string) (DriverStanding, error) {
	if err := f.err[id]; err != nil {
		return DriverStanding{}, err
	}

	if s, ok := f.standing[id]; ok {
		return s, nil
	}

	return DriverStanding{CanTakeTrips: true}, nil
}

func eligible(id string) DriverInfo {
	return DriverInfo{ID: id, Status: "active", AvailabilityStatus: "available", VehicleClass: "economy"}
}

type offersRig struct {
	trips   *offersTrips
	drivers *offersDrivers
	service Service
}

func newOffersRig(t *testing.T, ttl time.Duration, drivers map[string]DriverInfo, order []string, wallet offersWallet) offersRig {
	t.Helper()

	trips := &offersTrips{info: TripInfo{ID: "trip-1", Status: "requested", VehicleClass: "economy"}, offerResults: map[string]error{}}
	dr := &offersDrivers{info: drivers}

	return offersRig{
		trips:   trips,
		drivers: dr,
		service: NewService(trips, offersLocation{drivers: order}, dr, wallet, WithOffers(ttl)),
	}
}

func twoEligible() map[string]DriverInfo {
	return map[string]DriverInfo{"d1": eligible("d1"), "d2": eligible("d2"), "d3": eligible("d3")}
}

func TestByOffersTheNearestEligibleDriverIsOfferedTheTripAndNothingIsAssigned(t *testing.T) {
	rig := newOffersRig(t, 20*time.Second, twoEligible(), []string{"d1", "d2"}, offersWallet{})

	result, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Offered || result.DriverID != "d1" || result.TripID != "trip-1" || result.DistanceMeters != 100 {
		t.Errorf("unexpected result: %+v", result)
	}

	if len(rig.trips.offered) != 1 || rig.trips.offered[0] != "d1" || rig.trips.offeredTTL[0] != 20*time.Second {
		t.Errorf("expected one offer to d1 for the configured 20s, got %v %v", rig.trips.offered, rig.trips.offeredTTL)
	}

	if len(rig.trips.acceptedCalls) != 0 || len(rig.drivers.busy) != 0 {
		t.Errorf("an offer assigns nobody and marks nobody busy: accepted=%v busy=%v", rig.trips.acceptedCalls, rig.drivers.busy)
	}
}

func TestAnIneligibleDriverIsNeverOffered(t *testing.T) {
	drivers := map[string]DriverInfo{
		"d1": {ID: "d1", Status: "suspended", AvailabilityStatus: "available", VehicleClass: "economy"},
		"d2": {ID: "d2", Status: "active", AvailabilityStatus: "offline", VehicleClass: "economy"},
		"d3": {ID: "d3", Status: "active", AvailabilityStatus: "available", VehicleClass: "comfort"},
		"d4": eligible("d4"),
		"d5": eligible("d5"),
		"d6": eligible("d6"),
	}

	wallet := offersWallet{
		standing: map[string]DriverStanding{"d4": {CanTakeTrips: false, Suspended: true, Reason: "balance"}},
		err:      map[string]error{"d5": errors.New("wallet-service down")},
	}

	rig := newOffersRig(t, 15*time.Second, drivers, []string{"d1", "d2", "d3", "d4", "d5", "d6"}, wallet)

	result, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil || result.DriverID != "d6" || !result.Offered {
		t.Fatalf("only d6 is eligible: %+v, %v", result, err)
	}

	if len(rig.trips.offered) != 1 || rig.trips.offered[0] != "d6" {
		t.Errorf("only d6 may be offered the trip, got %v", rig.trips.offered)
	}
}

func TestADriverThatCannotBeOfferedIsSkippedAndTheNextOneIsOffered(t *testing.T) {
	for name, refusal := range map[string]error{
		"already offered before":           fmt.Errorf("%w: ALREADY_EXISTS", ErrDriverNotOfferable),
		"any other trouble with the offer": errors.New("trip-service unavailable"),
	} {
		rig := newOffersRig(t, 15*time.Second, twoEligible(), []string{"d1", "d2"}, offersWallet{})
		rig.trips.offerResults["d1"] = refusal

		result, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0)
		if err != nil || result.DriverID != "d2" || !result.Offered {
			t.Errorf("%s: the next driver must be offered it: %+v, %v", name, result, err)
		}

		if len(rig.trips.offered) != 2 || rig.trips.offered[0] != "d1" || rig.trips.offered[1] != "d2" {
			t.Errorf("%s: expected offers to d1 then d2, got %v", name, rig.trips.offered)
		}
	}
}

func TestAnotherDriversLiveOfferStopsTheSearchForNow(t *testing.T) {
	rig := newOffersRig(t, 15*time.Second, twoEligible(), []string{"d1", "d2", "d3"}, offersWallet{})
	rig.trips.offerResults["d1"] = fmt.Errorf("%w: ABORTED", ErrOfferPending)

	result, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, ErrOfferPending) || result.Offered {
		t.Fatalf("expected ErrOfferPending, got %+v, %v", result, err)
	}

	if len(rig.trips.offered) != 1 {
		t.Errorf("no other driver may be tried while an offer is live: %v", rig.trips.offered)
	}
}

func TestWhenEveryoneIsSkippedThereAreNoDriversAvailable(t *testing.T) {
	rig := newOffersRig(t, 15*time.Second, twoEligible(), []string{"d1", "d2"}, offersWallet{})
	rig.trips.offerResults["d1"] = ErrDriverNotOfferable
	rig.trips.offerResults["d2"] = ErrDriverNotOfferable

	if _, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0); !errors.Is(err, ErrNoDriversAvailable) {
		t.Errorf("expected ErrNoDriversAvailable, got %v", err)
	}

	if len(rig.trips.acceptedCalls) != 0 {
		t.Errorf("nobody may be assigned when offers fail: %v", rig.trips.acceptedCalls)
	}
}

func TestATripThatIsNoLongerWaitingIsNotOffered(t *testing.T) {
	rig := newOffersRig(t, 15*time.Second, twoEligible(), []string{"d1"}, offersWallet{})
	rig.trips.info.Status = "accepted"

	if _, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0); !errors.Is(err, ErrTripNotDispatchable) {
		t.Errorf("expected ErrTripNotDispatchable, got %v", err)
	}

	if len(rig.trips.offered) != 0 {
		t.Errorf("nothing may be offered: %v", rig.trips.offered)
	}
}

func TestWithoutOffersTheNearestDriverIsAssignedAsBefore(t *testing.T) {
	rig := newOffersRig(t, 0, twoEligible(), []string{"d1", "d2"}, offersWallet{})

	result, err := rig.service.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil || result.Offered || result.DriverID != "d1" {
		t.Fatalf("expected d1 assigned: %+v, %v", result, err)
	}

	if len(rig.trips.acceptedCalls) != 1 || rig.trips.acceptedCalls[0] != "d1" || len(rig.drivers.busy) != 1 || rig.drivers.busy[0] != "d1" {
		t.Errorf("assigning accepts the trip and marks the driver busy: accepted=%v busy=%v", rig.trips.acceptedCalls, rig.drivers.busy)
	}

	if len(rig.trips.offered) != 0 {
		t.Errorf("no offer may be made with offers off: %v", rig.trips.offered)
	}
}

func TestWithOffersIsCheckedWhenTheServiceIsBuilt(t *testing.T) {
	cases := map[string]struct {
		trips TripClient
		ttl   time.Duration
		panic bool
	}{
		"a negative ttl":                         {&offersTrips{}, -time.Second, true},
		"a client that cannot offer, offers on":  {offersTripsWithoutOffers{}, 15 * time.Second, true},
		"a client that cannot offer, offers off": {offersTripsWithoutOffers{}, 0, false},
		"a client that can offer, offers on":     {&offersTrips{}, 15 * time.Second, false},
	}

	for name, tc := range cases {
		func() {
			defer func() {
				if got := recover() != nil; got != tc.panic {
					t.Errorf("%s: panic=%v, expected %v", name, got, tc.panic)
				}
			}()

			NewService(tc.trips, offersLocation{}, &offersDrivers{}, offersWallet{}, WithOffers(tc.ttl))
		}()
	}
}
