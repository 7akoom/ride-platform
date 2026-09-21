package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type profileDirectoryFake struct {
	summary trip.DriverSummary
	err     error
	asked   []string
}

func (d *profileDirectoryFake) DriverSummary(_ context.Context, driverID string) (trip.DriverSummary, error) {
	d.asked = append(d.asked, driverID)

	return d.summary, d.err
}

type driverSummarizer interface {
	GetTripDriver(ctx context.Context, tripID string) (trip.DriverSummary, error)
}

type profileLocatorFake struct{}

func (profileLocatorFake) DriverLocation(context.Context, string) (trip.DriverLocation, error) {
	return trip.DriverLocation{UpdatedAt: time.Now()}, nil
}

func tripWithDriver(driverID string) trip.Trip {
	return trip.Trip{
		ID:       ratedTripID,
		RiderID:  ratedRider,
		DriverID: driverID,
		Status:   trip.StatusAccepted,
	}
}

func TestGetTripDriver_DescribesTheDriverOfTheTrip(t *testing.T) {
	directory := &profileDirectoryFake{summary: trip.DriverSummary{
		DisplayName:   "Ahmed",
		VehicleMake:   "Toyota",
		VehicleModel:  "Corolla",
		VehicleColor:  "White",
		PlateNumber:   "33452",
		RatingAverage: 4.8,
		RatingCount:   12,
	}}
	svc := trip.WithDriverProfile(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), directory)

	summarizer, ok := trip.As[driverSummarizer](svc)
	if !ok {
		t.Fatal("the decorator does not offer GetTripDriver")
	}

	got, err := summarizer.GetTripDriver(context.Background(), ratedTripID)
	if err != nil {
		t.Fatalf("GetTripDriver: %v", err)
	}

	if got.DisplayName != "Ahmed" || got.PlateNumber != "33452" || got.RatingCount != 12 {
		t.Errorf("unexpected summary: %+v", got)
	}

	if len(directory.asked) != 1 || directory.asked[0] != ratedDriver {
		t.Errorf("the directory was asked for %v, want the trip's driver %s", directory.asked, ratedDriver)
	}
}

func TestGetTripDriver_ATripWithoutADriverHasNobodyToDescribe(t *testing.T) {
	directory := &profileDirectoryFake{}
	svc := trip.WithDriverProfile(newRatingService(&ratingRepoFake{found: tripWithDriver("")}), directory)

	summarizer, _ := trip.As[driverSummarizer](svc)

	_, err := summarizer.GetTripDriver(context.Background(), ratedTripID)
	if !errors.Is(err, trip.ErrTripHasNoDriver) {
		t.Fatalf("got %v, want ErrTripHasNoDriver", err)
	}

	if len(directory.asked) != 0 {
		t.Error("the directory was asked although there is no driver")
	}
}

func TestGetTripDriver_FailuresReachTheCaller(t *testing.T) {
	t.Run("a trip that does not exist", func(t *testing.T) {
		svc := trip.WithDriverProfile(newRatingService(&ratingRepoFake{findErr: trip.ErrTripNotFound}), &profileDirectoryFake{})
		summarizer, _ := trip.As[driverSummarizer](svc)

		if _, err := summarizer.GetTripDriver(context.Background(), ratedTripID); !errors.Is(err, trip.ErrTripNotFound) {
			t.Fatalf("got %v, want ErrTripNotFound", err)
		}
	})

	t.Run("a driver whose profile cannot be read", func(t *testing.T) {
		directory := &profileDirectoryFake{err: trip.ErrDriverProfileUnavailable}
		svc := trip.WithDriverProfile(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), directory)
		summarizer, _ := trip.As[driverSummarizer](svc)

		if _, err := summarizer.GetTripDriver(context.Background(), ratedTripID); !errors.Is(err, trip.ErrDriverProfileUnavailable) {
			t.Fatalf("got %v, want ErrDriverProfileUnavailable", err)
		}
	})
}

func TestWithDriverProfile_StaysVisibleWhenAnotherDecoratorWrapsIt(t *testing.T) {
	profile := trip.WithDriverProfile(
		newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}),
		&profileDirectoryFake{},
	)
	stacked := trip.WithDriverTracking(profile, profileLocatorFake{})

	if _, ok := trip.As[driverSummarizer](stacked); !ok {
		t.Error("GetTripDriver is hidden once the tracking decorator wraps it")
	}

	type tracker interface {
		GetDriverLocation(ctx context.Context, tripID string) (trip.DriverLocation, error)
	}

	if _, ok := trip.As[tracker](stacked); !ok {
		t.Error("GetDriverLocation is no longer reachable")
	}
}

func TestWithDriverProfile_RejectsMissingDependencies(t *testing.T) {
	base := newRatingService(&ratingRepoFake{})

	for name, build := range map[string]func(){
		"no service":   func() { trip.WithDriverProfile(nil, &profileDirectoryFake{}) },
		"no directory": func() { trip.WithDriverProfile(base, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()

			build()
		})
	}
}
