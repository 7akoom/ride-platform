package dispatch

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"
)

var errTravelTimesCount = errors.New("the routing engine answered for a different number of origins")

// maxNextTripCandidates bounds how many busy drivers one dispatch attempt looks at
// for a next trip (each costs a trip lookup and a few road-time calls). They are
// the nearest ones.
const maxNextTripCandidates = 5

const driverBusy = "busy"

// CurrentTrip is a busy driver's trip as next trips need it: whether the rider is
// in the car, whether the driver already took a next trip, and where they are
// still going (the stops not reached yet, then the drop-off).
type CurrentTrip struct {
	TripID         string
	InProgress     bool
	HasNextTrip    bool
	RemainingStops []Point
	Dropoff        Point
}

// CurrentTripReader reads a driver's current trip (trip-service); found is false
// when they have none.
type CurrentTripReader interface {
	CurrentTrip(ctx context.Context, driverID string) (trip CurrentTrip, found bool, err error)
}

// NextTripSettings: Enabled lets a driver whose rider is in the car be offered a
// trip whose pickup is within RadiusMeters of where they drop that rider.
type NextTripSettings struct {
	Enabled      bool
	RadiusMeters float64
}

// NextTripSettingsSource reads the settings staff set (pricing-service).
type NextTripSettingsSource interface {
	NextTripSettings(ctx context.Context) (NextTripSettings, error)
}

// ChainedTripOfferer offers a trip with the road time to the pickup and, for a
// next trip, the driver's current trip. Refusals are those of TripOfferer.
type ChainedTripOfferer interface {
	OfferTripWithChain(ctx context.Context, tripID string, driverID string, ttl time.Duration, afterTripID string, pickupETA time.Duration) error
}

// WithNextTrips lets dispatch offer a trip to a driver who is finishing another
// one nearby, as the big ride apps do: their rider is in the car and they drop
// them within the settings' radius of the new pickup. Their time to the pickup is
// the rest of their trip by road plus the drive from the drop-off, and they are
// ranked with the free drivers by it.
//
// It works only with offers (a driver is never handed a second trip without
// accepting it), road ranking (the time must be known, not guessed) and a trip
// client that can make such offers; otherwise dispatch goes on as without it.
func WithNextTrips(trips CurrentTripReader, settings NextTripSettingsSource) Option {
	return func(s *service) {
		if trips == nil {
			panic("current trip reader is required")
		}

		if settings == nil {
			panic("next trip settings are required")
		}

		s.currentTrips = trips
		s.nextTripSettings = settings
	}
}

// queued is one driver to try, in order: afterTripID is set for a next trip.
type queued struct {
	candidate   NearbyDriver
	afterTripID string
}

// driverLookup is a driver read once for this attempt.
type driverLookup struct {
	info DriverInfo
	err  error
}

// nextTripSettingsFor is whether next trips are on for this attempt, and how near.
func (s *service) nextTripSettingsFor(ctx context.Context, tripID string) (NextTripSettings, bool) {
	if s.currentTrips == nil || s.nextTripSettings == nil || s.offerTTL <= 0 || s.travelTimes == nil {
		return NextTripSettings{}, false
	}

	if _, ok := s.tripClient.(ChainedTripOfferer); !ok {
		return NextTripSettings{}, false
	}

	settings, err := s.nextTripSettings.NextTripSettings(ctx)
	if err != nil {
		s.log().WarnContext(ctx, "dispatch attempt: next trip settings unavailable, offering free drivers only",
			"trip_id", tripID,
			"error", err,
		)

		return NextTripSettings{}, false
	}

	return settings, settings.Enabled && settings.RadiusMeters > 0
}

// orderCandidates is the order to try drivers in. Without next trips it is the
// road ranking of every nearby driver. With them, every nearby driver is read
// first, so the busy ones who may take a next trip are ranked by their own time
// (the rest of their trip, then on to this pickup) among the free ones.
func (s *service) orderCandidates(
	ctx context.Context,
	tripID string,
	tripInfo TripInfo,
	pickup Point,
	candidates []NearbyDriver,
	skip func(driverID string, format string, args ...any),
) ([]queued, map[string]driverLookup) {
	settings, on := s.nextTripSettingsFor(ctx, tripID)
	if !on {
		return queueOf(s.rankByRoad(ctx, tripID, pickup, candidates, skip)), nil
	}

	lookups := make(map[string]driverLookup, len(candidates))
	tripClass := effectiveVehicleClass(tripInfo.VehicleClass)

	var free, busy []NearbyDriver

	for _, candidate := range candidates {
		info, err := s.driverClient.GetDriver(ctx, candidate.DriverID)
		lookups[candidate.DriverID] = driverLookup{info: info, err: err}

		mayTakeNext := err == nil &&
			info.Status == driverStatusActive &&
			info.AvailabilityStatus == driverBusy &&
			effectiveVehicleClass(info.VehicleClass) == tripClass

		if mayTakeNext && len(busy) < maxNextTripCandidates {
			busy = append(busy, candidate)

			continue
		}

		free = append(free, candidate)
	}

	ranked := s.rankByRoad(ctx, tripID, pickup, free, skip)
	next := s.planNextTrips(ctx, tripID, pickup, busy, settings.RadiusMeters, skip)

	return mergeQueue(ranked, next), lookups
}

// planNextTrips keeps the busy drivers who may take this trip after theirs, with
// their time to the pickup: their rider is in the car, they have no next trip yet,
// they drop the rider within radius of the pickup, and the road times are known.
func (s *service) planNextTrips(
	ctx context.Context,
	tripID string,
	pickup Point,
	busy []NearbyDriver,
	radiusMeters float64,
	skip func(driverID string, format string, args ...any),
) []queued {
	type pending struct {
		candidate   NearbyDriver
		afterTripID string
		toDropoff   float64
		dropoff     Point
	}

	var waiting []pending

	for _, candidate := range busy {
		current, found, err := s.currentTrips.CurrentTrip(ctx, candidate.DriverID)

		switch {
		case err != nil:
			skip(candidate.DriverID, "busy: could not read their trip: %v", err)

			continue
		case !found:
			skip(candidate.DriverID, "busy without a current trip")

			continue
		case !current.InProgress:
			skip(candidate.DriverID, "busy: still on the way to their rider")

			continue
		case current.HasNextTrip:
			skip(candidate.DriverID, "busy: already took a next trip")

			continue
		}

		if gap := distanceMeters(current.Dropoff, pickup); gap > radiusMeters {
			skip(candidate.DriverID, "busy: drops their rider %.0f m from the pickup (at most %.0f m)", gap, radiusMeters)

			continue
		}

		path := append([]Point{{Latitude: candidate.Latitude, Longitude: candidate.Longitude}}, current.RemainingStops...)
		path = append(path, current.Dropoff)

		toDropoff, ok := s.travelAlong(ctx, path)
		if !ok {
			skip(candidate.DriverID, "busy: time to their drop-off by road is not known")

			continue
		}

		waiting = append(waiting, pending{candidate: candidate, afterTripID: current.TripID, toDropoff: toDropoff, dropoff: current.Dropoff})
	}

	if len(waiting) == 0 {
		return nil
	}

	origins := make([]Point, len(waiting))
	for i, w := range waiting {
		origins[i] = w.dropoff
	}

	times, err := s.travelTimesTo(ctx, origins, pickup)
	if err != nil {
		for _, w := range waiting {
			skip(w.candidate.DriverID, "busy: time from their drop-off to the pickup is not known: %v", err)
		}

		return nil
	}

	var planned []queued

	for i, w := range waiting {
		if !times[i].Reachable {
			skip(w.candidate.DriverID, "busy: no road from their drop-off to the pickup")

			continue
		}

		total := w.toDropoff + times[i].DurationSeconds

		if s.maxPickup > 0 && total > s.maxPickup.Seconds() {
			skip(w.candidate.DriverID, "busy: too far by road (%.0f s to the pickup after their trip, at most %.0f s)",
				total, s.maxPickup.Seconds())

			continue
		}

		candidate := w.candidate
		candidate.PickupETASeconds = total
		planned = append(planned, queued{candidate: candidate, afterTripID: w.afterTripID})
	}

	return planned
}

// travelAlong is the road time through the points in order; false when a leg is
// not known.
func (s *service) travelAlong(ctx context.Context, path []Point) (float64, bool) {
	var total float64

	for i := 0; i+1 < len(path); i++ {
		times, err := s.travelTimesTo(ctx, path[i:i+1], path[i+1])
		if err != nil || !times[0].Reachable {
			return 0, false
		}

		total += times[0].DurationSeconds
	}

	return total, true
}

func (s *service) travelTimesTo(ctx context.Context, origins []Point, destination Point) ([]TravelTime, error) {
	callCtx, cancel := context.WithTimeout(ctx, roadRankingTimeout)
	defer cancel()

	times, err := s.travelTimes.TravelTimes(callCtx, origins, destination)
	if err != nil {
		return nil, err
	}

	if len(times) != len(origins) {
		return nil, errTravelTimesCount
	}

	return times, nil
}

// mergeQueue ranks next trips among the free drivers whose road time is known,
// fastest first (a free driver first on equal times), with the free drivers of
// unknown time after them. When no free driver's time is known (the road ranking
// failed) the free drivers keep their straight-line order and next trips come
// after them.
func mergeQueue(ranked []NearbyDriver, next []queued) []queued {
	var known, unknown []queued

	for _, candidate := range ranked {
		if candidate.PickupETASeconds > 0 {
			known = append(known, queued{candidate: candidate})
		} else {
			unknown = append(unknown, queued{candidate: candidate})
		}
	}

	if len(known) == 0 && len(unknown) > 0 {
		return append(unknown, next...)
	}

	known = append(known, next...)
	sort.SliceStable(known, func(i, j int) bool {
		return known[i].candidate.PickupETASeconds < known[j].candidate.PickupETASeconds
	})

	return append(known, unknown...)
}

func queueOf(candidates []NearbyDriver) []queued {
	out := make([]queued, len(candidates))
	for i, candidate := range candidates {
		out[i] = queued{candidate: candidate}
	}

	return out
}

// lookupDriver reads the driver, or takes them from this attempt's lookups.
func (s *service) lookupDriver(ctx context.Context, lookups map[string]driverLookup, driverID string) (DriverInfo, error) {
	if found, ok := lookups[driverID]; ok {
		return found.info, found.err
	}

	return s.driverClient.GetDriver(ctx, driverID)
}

const earthRadiusMeters = 6371000

// distanceMeters is the great-circle distance between two points.
func distanceMeters(a, b Point) float64 {
	lat1, lat2 := a.Latitude*math.Pi/180, b.Latitude*math.Pi/180
	dLat := lat2 - lat1
	dLng := (b.Longitude - a.Longitude) * math.Pi / 180

	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)

	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(h))
}
