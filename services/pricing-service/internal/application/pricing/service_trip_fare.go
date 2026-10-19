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
