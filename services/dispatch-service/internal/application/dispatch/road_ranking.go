package dispatch

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Point is a position in degrees.
type Point struct {
	Latitude  float64
	Longitude float64
}

// TravelTime is how long by road from a driver to the pickup.
type TravelTime struct {
	// Reachable is false when there is no known way by road (no route, or the
	// driver's position is too far from a road to tell).
	Reachable       bool
	DurationSeconds float64
	DistanceMeters  float64
}

// TravelTimeClient is Dispatch's view of the routing engine (location-service's
// GetTravelTimes): how long by road from each origin to one destination, in the
// origins' order.
type TravelTimeClient interface {
	TravelTimes(ctx context.Context, origins []Point, destination Point) ([]TravelTime, error)
}

// roadRankingTimeout bounds the routing call: a slow map service falls back to the
// straight-line order rather than holding the rider up.
const roadRankingTimeout = 2 * time.Second

// WithRoadRanking makes dispatch try nearby drivers by how long they take to reach
// the pickup by road, as the big ride apps do, instead of by straight-line
// distance: the nearest driver on the map may be across a river or a highway, or
// facing the wrong way on a divided road.
//
// maxPickup, when positive, leaves out drivers known to be further than that by
// road (Uber's "max dispatch ETA"): the trip waits for a closer driver rather than
// having the rider wait half an hour. Drivers whose time is not known are still
// tried, after the ones whose time is.
//
// When the routing engine cannot answer, dispatch goes on in straight-line order:
// ranking is about choosing well, not about whether a driver may take the trip.
func WithRoadRanking(client TravelTimeClient, maxPickup time.Duration) Option {
	return func(s *service) {
		if client == nil {
			panic("travel time client is required")
		}

		if maxPickup < 0 {
			panic("max pickup time must not be negative")
		}

		s.travelTimes = client
		s.maxPickup = maxPickup
	}
}

// rankByRoad orders the candidates by their time to the pickup (known times first,
// fastest first; unknown ones after, nearest first) and drops those known to be
// further than maxPickup, recording why.
func (s *service) rankByRoad(
	ctx context.Context,
	tripID string,
	pickup Point,
	candidates []NearbyDriver,
	skip func(driverID string, format string, args ...any),
) []NearbyDriver {
	if s.travelTimes == nil || len(candidates) == 0 {
		return candidates
	}

	origins := make([]Point, len(candidates))
	for i, candidate := range candidates {
		origins[i] = Point{Latitude: candidate.Latitude, Longitude: candidate.Longitude}
	}

	callCtx, cancel := context.WithTimeout(ctx, roadRankingTimeout)
	times, err := s.travelTimes.TravelTimes(callCtx, origins, pickup)
	cancel()

	if err == nil && len(times) != len(candidates) {
		err = fmt.Errorf("asked for %d travel times, got %d", len(candidates), len(times))
	}

	if err != nil {
		s.log().WarnContext(ctx, "dispatch attempt: travel times unavailable, trying drivers in straight-line order",
			"trip_id", tripID,
			"error", err,
		)

		return candidates
	}

	var known, unknown []NearbyDriver

	for i, candidate := range candidates {
		t := times[i]
		if !t.Reachable {
			unknown = append(unknown, candidate)

			continue
		}

		if s.maxPickup > 0 && t.DurationSeconds > s.maxPickup.Seconds() {
			skip(candidate.DriverID, "too far by road (%.0f s to the pickup, at most %.0f s)",
				t.DurationSeconds, s.maxPickup.Seconds())

			continue
		}

		candidate.PickupETASeconds = t.DurationSeconds
		known = append(known, candidate)
	}

	// Equal times keep the straight-line order (the input is nearest first).
	sort.SliceStable(known, func(i, j int) bool {
		return known[i].PickupETASeconds < known[j].PickupETASeconds
	})

	return append(known, unknown...)
}
