package dispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Erbil-ish points. The new rider waits at pickup; "near" is a drop-off 1.1 km from
// it, "far" one 5.5 km away.
var (
	pickup      = Point{Latitude: 36.1900, Longitude: 44.0100}
	nearDropoff = Point{Latitude: 36.2000, Longitude: 44.0100}
	farDropoff  = Point{Latitude: 36.2400, Longitude: 44.0100}
	stopOnWay   = Point{Latitude: 36.2100, Longitude: 44.0300}
)

// chainTrips can make offers after another trip.
type chainTrips struct {
	offersTrips

	afterTrip []string
	etas      []time.Duration
}

func (f *chainTrips) OfferTripWithChain(_ context.Context, _ string, driverID string, ttl time.Duration, afterTripID string, eta time.Duration) error {
	f.offered = append(f.offered, driverID)
	f.offeredTTL = append(f.offeredTTL, ttl)
	f.afterTrip = append(f.afterTrip, afterTripID)
	f.etas = append(f.etas, eta)

	return f.offerResults[driverID]
}

type placedLocation struct{ drivers []NearbyDriver }

func (f placedLocation) FindNearbyDrivers(context.Context, float64, float64, float64, int) ([]NearbyDriver, error) {
	return f.drivers, nil
}

// legTravel answers road times by leg; a leg it does not know is unreachable.
type legTravel struct {
	seconds map[string]float64
	err     error
}

func leg(from, to Point) string {
	return fmt.Sprintf("%.4f,%.4f>%.4f,%.4f", from.Latitude, from.Longitude, to.Latitude, to.Longitude)
}

func (f legTravel) TravelTimes(_ context.Context, origins []Point, destination Point) ([]TravelTime, error) {
	if f.err != nil {
		return nil, f.err
	}

	out := make([]TravelTime, len(origins))
	for i, origin := range origins {
		if seconds, ok := f.seconds[leg(origin, destination)]; ok {
			out[i] = TravelTime{Reachable: true, DurationSeconds: seconds}
		}
	}

	return out, nil
}

type currentTrips struct {
	trips map[string]CurrentTrip
	err   error
	asked []string
}

func (f *currentTrips) CurrentTrip(_ context.Context, driverID string) (CurrentTrip, bool, error) {
	f.asked = append(f.asked, driverID)

	if f.err != nil {
		return CurrentTrip{}, false, f.err
	}

	trip, ok := f.trips[driverID]

	return trip, ok, nil
}

type fixedSettings struct {
	settings NextTripSettings
	err      error
}

func (f fixedSettings) NextTripSettings(context.Context) (NextTripSettings, error) {
	return f.settings, f.err
}

func busyDriver(id string) DriverInfo {
	return DriverInfo{ID: id, Status: "active", AvailabilityStatus: "busy", VehicleClass: "economy"}
}

type nextRig struct {
	trips    *chainTrips
	current  *currentTrips
	travel   legTravel
	settings fixedSettings
	drivers  map[string]DriverInfo
	nearby   []NearbyDriver
	ttl      time.Duration
	max      time.Duration
}

// newNextRig: "free" is an available driver 10 min from the pickup by road; "busy"
// has their rider in the car, 4 min from a drop-off 2 min from the pickup.
func newNextRig() *nextRig {
	freeAt := Point{Latitude: 36.1500, Longitude: 44.0100}
	busyAt := Point{Latitude: 36.2200, Longitude: 44.0200}

	return &nextRig{
		trips: &chainTrips{offersTrips: offersTrips{
			info:         TripInfo{ID: "trip-new", Status: "requested", VehicleClass: "economy", PickupLat: pickup.Latitude, PickupLng: pickup.Longitude},
			offerResults: map[string]error{},
		}},
		current: &currentTrips{trips: map[string]CurrentTrip{
			"busy": {TripID: "trip-old", InProgress: true, Dropoff: nearDropoff},
		}},
		travel: legTravel{seconds: map[string]float64{
			leg(freeAt, pickup):      600,
			leg(busyAt, nearDropoff): 240,
			leg(nearDropoff, pickup): 120,
		}},
		settings: fixedSettings{settings: NextTripSettings{Enabled: true, RadiusMeters: 3000}},
		drivers:  map[string]DriverInfo{"free": eligible("free"), "busy": busyDriver("busy")},
		nearby: []NearbyDriver{
			{DriverID: "busy", DistanceMeters: 3000, Latitude: busyAt.Latitude, Longitude: busyAt.Longitude},
			{DriverID: "free", DistanceMeters: 4500, Latitude: freeAt.Latitude, Longitude: freeAt.Longitude},
		},
		ttl: 15 * time.Second,
		max: 20 * time.Minute,
	}
}

func (r *nextRig) dispatch(t *testing.T) (Result, error) {
	t.Helper()

	service := NewService(r.trips, placedLocation{drivers: r.nearby}, &offersDrivers{info: r.drivers}, offersWallet{},
		WithOffers(r.ttl), WithRoadRanking(r.travel, r.max), WithNextTrips(r.current, r.settings))

	return service.DispatchTrip(context.Background(), "trip-new", 0)
}

func TestADriverFinishingNearbyIsOfferedTheTripWhenTheyAreSooner(t *testing.T) {
	rig := newNextRig()

	result, err := rig.dispatch(t)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "busy" || result.AfterTripID != "trip-old" || result.PickupETASeconds != 360 {
		t.Fatalf("result %+v", result)
	}

	if rig.trips.afterTrip[0] != "trip-old" || rig.trips.etas[0] != 6*time.Minute {
		t.Fatalf("offered after %v in %v", rig.trips.afterTrip, rig.trips.etas)
	}
}

func TestAFreeDriverWhoIsSoonerComesFirst(t *testing.T) {
	rig := newNextRig()
	rig.travel.seconds[leg(Point{Latitude: 36.1500, Longitude: 44.0100}, pickup)] = 300

	result, err := rig.dispatch(t)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "free" || result.AfterTripID != "" || rig.trips.etas[0] != 5*time.Minute {
		t.Fatalf("result %+v, etas %v", result, rig.trips.etas)
	}
}

func TestOnEqualTimesTheFreeDriverComesFirst(t *testing.T) {
	rig := newNextRig()
	rig.travel.seconds[leg(Point{Latitude: 36.1500, Longitude: 44.0100}, pickup)] = 360

	if result, _ := rig.dispatch(t); result.DriverID != "free" {
		t.Fatalf("result %+v", result)
	}
}

func TestTheBusyDriverIsLeftOutWhen(t *testing.T) {
	cases := map[string]func(r *nextRig){
		"they drop their rider too far from the pickup": func(r *nextRig) {
			r.current.trips["busy"] = CurrentTrip{TripID: "trip-old", InProgress: true, Dropoff: farDropoff}
		},
		"they are still on the way to their rider": func(r *nextRig) {
			r.current.trips["busy"] = CurrentTrip{TripID: "trip-old", Dropoff: nearDropoff}
		},
		"they already took a next trip": func(r *nextRig) {
			r.current.trips["busy"] = CurrentTrip{TripID: "trip-old", InProgress: true, HasNextTrip: true, Dropoff: nearDropoff}
		},
		"their trip cannot be read": func(r *nextRig) { r.current.err = errors.New("trip-service down") },
		"the road from their drop-off is not known": func(r *nextRig) {
			delete(r.travel.seconds, leg(nearDropoff, pickup))
		},
		"the whole way is too long": func(r *nextRig) {
			r.max = 5 * time.Minute
			r.travel.seconds[leg(Point{Latitude: 36.1500, Longitude: 44.0100}, pickup)] = 240
		},
		"next trips are switched off": func(r *nextRig) { r.settings.settings.Enabled = false },
		"the settings cannot be read": func(r *nextRig) { r.settings.err = errors.New("pricing down") },
		"dispatch assigns instead of offering": func(r *nextRig) {
			r.ttl = 0
		},
		"they drive another class": func(r *nextRig) {
			info := busyDriver("busy")
			info.VehicleClass = "comfort"
			r.drivers["busy"] = info
		},
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newNextRig()
			change(rig)

			result, err := rig.dispatch(t)
			if err != nil {
				t.Fatal(err)
			}

			if result.DriverID != "free" || result.AfterTripID != "" {
				t.Fatalf("result %+v", result)
			}

			for _, after := range rig.trips.afterTrip {
				if after != "" {
					t.Fatalf("a next trip was offered: %v", rig.trips.afterTrip)
				}
			}
		})
	}
}

func TestTheWayToTheDropoffGoesThroughTheStopsLeft(t *testing.T) {
	rig := newNextRig()
	busyAt := Point{Latitude: 36.2200, Longitude: 44.0200}
	rig.current.trips["busy"] = CurrentTrip{TripID: "trip-old", InProgress: true, RemainingStops: []Point{stopOnWay}, Dropoff: nearDropoff}
	rig.travel.seconds[leg(busyAt, stopOnWay)] = 60
	rig.travel.seconds[leg(stopOnWay, nearDropoff)] = 90

	result, err := rig.dispatch(t)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "busy" || result.PickupETASeconds != 270 {
		t.Fatalf("result %+v (want 60 + 90 + 120)", result)
	}
}

func TestWhenTheFreeDriversTimesAreUnknownNextTripsComeAfterThem(t *testing.T) {
	ranked := []NearbyDriver{{DriverID: "a"}, {DriverID: "b"}}
	next := []queued{{candidate: NearbyDriver{DriverID: "busy", PickupETASeconds: 60}, afterTripID: "trip-old"}}

	got := mergeQueue(ranked, next)
	if len(got) != 3 || got[0].candidate.DriverID != "a" || got[2].candidate.DriverID != "busy" {
		t.Fatalf("order %+v", got)
	}
}

func TestDistanceMeters(t *testing.T) {
	if got := distanceMeters(pickup, nearDropoff); got < 1100 || got > 1125 {
		t.Fatalf("%.0f m", got)
	}
}
