package trip_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type tripCounterFake struct {
	riders  map[string]int
	drivers map[string]int
	err     error
}

func (f *tripCounterFake) CountRiderCompletedTrips(_ context.Context, riderID string) (int, error) {
	return f.riders[riderID], f.err
}

func (f *tripCounterFake) CountDriverCompletedTrips(_ context.Context, driverID string) (int, error) {
	return f.drivers[driverID], f.err
}

type riderTripCounter interface {
	RiderCompletedTrips(ctx context.Context, riderID string) (int, error)
}

type driverTripCounter interface {
	DriverCompletedTripsOfTrip(ctx context.Context, tripID string) (int, error)
}

func TestTripCounts_CountTheRidersAndTheTripsDriversCompletedTrips(t *testing.T) {
	counter := &tripCounterFake{riders: map[string]int{ratedRider: 7}, drivers: map[string]int{ratedDriver: 1240}}
	svc := trip.WithTripCounts(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), counter)

	riders, ok := trip.As[riderTripCounter](svc)
	if !ok {
		t.Fatal("the decorator does not offer RiderCompletedTrips")
	}

	if got, err := riders.RiderCompletedTrips(context.Background(), " "+ratedRider+" "); err != nil || got != 7 {
		t.Fatalf("rider trips = %d, %v; want 7", got, err)
	}

	if _, err := riders.RiderCompletedTrips(context.Background(), "  "); !errors.Is(err, trip.ErrRiderIDRequired) {
		t.Fatalf("no rider id: err = %v, want ErrRiderIDRequired", err)
	}

	drivers, ok := trip.As[driverTripCounter](svc)
	if !ok {
		t.Fatal("the decorator does not offer DriverCompletedTripsOfTrip")
	}

	if got, err := drivers.DriverCompletedTripsOfTrip(context.Background(), ratedTripID); err != nil || got != 1240 {
		t.Fatalf("driver trips = %d, %v; want 1240", got, err)
	}
}

func TestTripCounts_ATripWithoutADriverHasNoDriverCount(t *testing.T) {
	svc := trip.WithTripCounts(newRatingService(&ratingRepoFake{found: tripWithDriver("")}), &tripCounterFake{})

	drivers, _ := trip.As[driverTripCounter](svc)

	if _, err := drivers.DriverCompletedTripsOfTrip(context.Background(), ratedTripID); !errors.Is(err, trip.ErrTripHasNoDriver) {
		t.Fatalf("err = %v, want ErrTripHasNoDriver", err)
	}
}

func TestTripCounts_AStoreFailureIsReturned(t *testing.T) {
	broken := errors.New("database down")
	svc := trip.WithTripCounts(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), &tripCounterFake{err: broken})

	riders, _ := trip.As[riderTripCounter](svc)

	if _, err := riders.RiderCompletedTrips(context.Background(), ratedRider); !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the store's", err)
	}
}

func TestWithTripCounts_PanicsWithoutItsParts(t *testing.T) {
	for name, build := range map[string]func(){
		"no service": func() { trip.WithTripCounts(nil, &tripCounterFake{}) },
		"no counter": func() {
			trip.WithTripCounts(newRatingService(&ratingRepoFake{}), nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()

			build()
		})
	}
}
