package pricing

import (
	"context"
	"fmt"
	"strings"
)

// TripFare is the fare recorded for one of the rider's trips: what the trip cost,
// or the fee a cancelled trip owed. A trip that is not the rider's is not found,
// as one with no fare is: the rider cannot learn that another's trip exists.
func (s *service) TripFare(ctx context.Context, riderID, tripID string) (Fare, error) {
	riderID = strings.TrimSpace(riderID)
	tripID = strings.TrimSpace(tripID)

	if riderID == "" {
		return Fare{}, ErrRiderIDRequired
	}

	if tripID == "" {
		return Fare{}, ErrTripIDRequired
	}

	// looksLikeUUID (service_calculate.go): anything else could not match, and
	// would make Postgres refuse the query.
	if !looksLikeUUID(tripID) {
		return Fare{}, ErrFareNotFound
	}

	fare, found, err := s.repository.FindFareByTripID(ctx, tripID)
	if err != nil {
		return Fare{}, fmt.Errorf("find fare: %w", err)
	}

	if !found || fare.RiderID != riderID {
		return Fare{}, ErrFareNotFound
	}

	return fare, nil
}

// MaxTripFares is the most trips TripFares takes at once (two pages of a trip list).
const MaxTripFares = 100

// TripFares is the recorded fares of the trips named, for trip-service's lists. Ids
// that are not UUIDs are skipped (they have no fare); repeats are asked once.
func (s *service) TripFares(ctx context.Context, tripIDs []string) ([]Fare, error) {
	if len(tripIDs) > MaxTripFares {
		return nil, ErrTooManyTrips
	}

	seen := make(map[string]bool, len(tripIDs))
	ids := make([]string, 0, len(tripIDs))

	for _, id := range tripIDs {
		id = strings.TrimSpace(id)
		if looksLikeUUID(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	fares, err := s.repository.ListFaresByTripIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list fares: %w", err)
	}

	return fares, nil
}
