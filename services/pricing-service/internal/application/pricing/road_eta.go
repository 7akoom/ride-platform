package pricing

import (
	"context"
	"math"
)

// TravelTime is how long by road from a driver to the pickup.
type TravelTime struct {
	Reachable       bool
	DurationSeconds float64
}

// TravelTimer is location-service's GetTravelTimes: how long by road from each
// origin to one destination, in the origins' order, in one call.
type TravelTimer interface {
	TravelTimes(ctx context.Context, origins []Point, destination Point) ([]TravelTime, error)
}

// WithTravelTimes makes each quote's pickup time that of the free driver of its
// class who is fastest to the pickup by road (the one dispatch would send), asked
// for all nearby drivers at once. Without it, or when the call fails, the time is
// the road route of the nearest driver in a straight line, as before.
func WithTravelTimes(timer TravelTimer) Option {
	return func(s *service) {
		if timer == nil {
			panic("travel timer is required")
		}

		s.travelTimer = timer
	}
}

// roadETAs is the time by road of each free driver near the pickup, in minutes
// (rounded up, at least 1), in the order of m.drivers; -1 for a driver with no way
// by road. It is nil when the times are not known.
func (s *service) roadETAs(ctx context.Context, m market) []int {
	if s.travelTimer == nil || !m.driversKnown || len(m.drivers) == 0 {
		return nil
	}

	origins := make([]Point, len(m.drivers))
	for i, driver := range m.drivers {
		origins[i] = driver.Location
	}

	callCtx, cancel := context.WithTimeout(ctx, etaTimeout)
	defer cancel()

	times, err := s.travelTimer.TravelTimes(callCtx, origins,
		Point{Latitude: m.request.PickupLat, Longitude: m.request.PickupLng})
	if err != nil || len(times) != len(m.drivers) {
		return nil
	}

	minutes := make([]int, len(times))

	for i, t := range times {
		if !t.Reachable {
			minutes[i] = -1

			continue
		}

		minutes[i] = max(1, int(math.Ceil(t.DurationSeconds/60)))
	}

	return minutes
}
