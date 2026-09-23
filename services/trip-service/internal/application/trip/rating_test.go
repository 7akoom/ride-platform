package trip_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// ratingRepoFake implements the whole Repository, but only FindByID and CreateRating do
// anything: it is a self-contained double for rating a trip.
type ratingRepoFake struct {
	found    trip.Trip
	findErr  error
	createFn func(trip.CreateRatingInput) error

	asked   []string
	created []trip.CreateRatingInput
}

func (r *ratingRepoFake) Create(context.Context, trip.CreateInput) (trip.Trip, error) {
	return trip.Trip{}, nil
}

func (r *ratingRepoFake) FindByID(_ context.Context, tripID string) (trip.Trip, error) {
	r.asked = append(r.asked, tripID)

	return r.found, r.findErr
}

func (r *ratingRepoFake) FindActiveByRiderID(context.Context, string) (trip.Trip, error) {
	return trip.Trip{}, trip.ErrTripNotFound
}

func (r *ratingRepoFake) FindActiveByDriverID(context.Context, string) (trip.Trip, error) {
	return trip.Trip{}, trip.ErrTripNotFound
}

func (r *ratingRepoFake) Accept(context.Context, string, string) (trip.Trip, error) {
	return trip.Trip{}, nil
}

func (r *ratingRepoFake) Start(context.Context, string) (trip.Trip, error) { return trip.Trip{}, nil }

func (r *ratingRepoFake) Complete(context.Context, string) (trip.Trip, error) {
	return trip.Trip{}, nil
}

func (r *ratingRepoFake) Cancel(context.Context, trip.CancelRecord) (trip.Trip, error) {
	return trip.Trip{}, nil
}

func (r *ratingRepoFake) MarkArrived(context.Context, string) (trip.Trip, error) {
	return trip.Trip{}, nil
}

func (r *ratingRepoFake) TriggerSOS(
	context.Context, string, trip.SosTriggeredBy, trip.Coordinates,
) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (r *ratingRepoFake) RecordWaypointIfDue(
	context.Context, string, trip.Coordinates, time.Time, time.Duration,
) error {
	return nil
}

func (r *ratingRepoFake) ListWaypoints(context.Context, string) ([]trip.Waypoint, error) {
	return nil, nil
}

func (r *ratingRepoFake) CreateRating(_ context.Context, in trip.CreateRatingInput) (trip.Rating, error) {
	r.created = append(r.created, in)

	if r.createFn != nil {
		if err := r.createFn(in); err != nil {
			return trip.Rating{}, err
		}
	}

	return trip.Rating{
		ID:      in.ID,
		TripID:  in.TripID,
		RatedBy: in.RatedBy,
		RaterID: in.RaterID,
		RateeID: in.RateeID,
		Stars:   in.Stars,
		Comment: in.Comment,
	}, nil
}

type ratingIDs struct{}

func (ratingIDs) NewID() string { return "rating-1" }

type ratingZones struct{}

func (ratingZones) CheckServiceZone(context.Context, float64, float64) (bool, error) {
	return true, nil
}

const (
	ratedTripID = "3f2b6a7e-1c1d-4b5e-9a3f-0d8c6b1a2e4f"
	ratedRider  = "d586ce00-5c1c-46f1-81b5-ed7e0977d075"
	ratedDriver = "fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
)

func completedTripAgo(age time.Duration) trip.Trip {
	completedAt := time.Now().Add(-age)

	return trip.Trip{
		ID:          ratedTripID,
		RiderID:     ratedRider,
		DriverID:    ratedDriver,
		Status:      trip.StatusCompleted,
		CompletedAt: &completedAt,
	}
}

func newRatingService(repo *ratingRepoFake) trip.Service {
	return trip.NewService(repo, ratingIDs{}, ratingZones{})
}

func rateAs(svc trip.Service, by trip.RatedBy, stars int, comment string) (trip.Rating, error) {
	return svc.RateTrip(context.Background(), trip.RateTripInput{
		TripID:  ratedTripID,
		RatedBy: by,
		Stars:   stars,
		Comment: comment,
	})
}

func TestRateTrip_TheRiderRatesTheDriver(t *testing.T) {
	repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}

	got, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, "  great driver  ")
	if err != nil {
		t.Fatalf("RateTrip: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("ratings written = %d, want 1", len(repo.created))
	}

	in := repo.created[0]

	if in.RaterID != ratedRider || in.RateeID != ratedDriver {
		t.Errorf("rater/ratee = %s/%s, want the rider rating the driver", in.RaterID, in.RateeID)
	}

	if in.Stars != 5 || in.Comment != "great driver" || in.ID != "rating-1" || in.TripID != ratedTripID {
		t.Errorf("unexpected rating written: %+v", in)
	}

	if got.RatedBy != trip.RatedByRider {
		t.Errorf("rated by = %q, want rider", got.RatedBy)
	}
}

func TestRateTrip_TheDriverRatesTheRider(t *testing.T) {
	repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}

	if _, err := rateAs(newRatingService(repo), trip.RatedByDriver, 4, ""); err != nil {
		t.Fatalf("RateTrip: %v", err)
	}

	in := repo.created[0]

	if in.RaterID != ratedDriver || in.RateeID != ratedRider {
		t.Errorf("rater/ratee = %s/%s, want the driver rating the rider", in.RaterID, in.RateeID)
	}

	if in.Comment != "" {
		t.Errorf("comment = %q, want empty", in.Comment)
	}
}

func TestRateTrip_InputErrorsNeverReachTheRepository(t *testing.T) {
	cases := []struct {
		name    string
		tripID  string
		by      trip.RatedBy
		stars   int
		comment string
		want    error
	}{
		{"blank trip id", "  ", trip.RatedByRider, 5, "", trip.ErrTripIDRequired},
		{"no side named", ratedTripID, "", 5, "", trip.ErrInvalidRatedBy},
		{"an unknown side", ratedTripID, trip.RatedBy("admin"), 5, "", trip.ErrInvalidRatedBy},
		{"zero stars", ratedTripID, trip.RatedByRider, 0, "", trip.ErrInvalidStars},
		{"six stars", ratedTripID, trip.RatedByRider, 6, "", trip.ErrInvalidStars},
		{"negative stars", ratedTripID, trip.RatedByRider, -1, "", trip.ErrInvalidStars},
		{"a comment of 501 characters", ratedTripID, trip.RatedByRider, 5, strings.Repeat("a", 501), trip.ErrRatingCommentTooLong},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}

			_, err := newRatingService(repo).RateTrip(context.Background(), trip.RateTripInput{
				TripID: tc.tripID, RatedBy: tc.by, Stars: tc.stars, Comment: tc.comment,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(repo.asked) != 0 || len(repo.created) != 0 {
				t.Error("the repository was used for an invalid request")
			}
		})
	}
}

func TestRateTrip_TheCommentLimitCountsCharactersNotBytes(t *testing.T) {
	// 500 Arabic letters are 1000 bytes: they must still fit; 501 must not.
	fits := strings.Repeat("ع", 500)
	tooLong := strings.Repeat("ع", 501)

	repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}

	if _, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, fits); err != nil {
		t.Errorf("500 characters: %v", err)
	}

	if _, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, tooLong); !errors.Is(err, trip.ErrRatingCommentTooLong) {
		t.Errorf("501 characters: got %v, want ErrRatingCommentTooLong", err)
	}
}

func TestRateTrip_OnlyACompletedTripCanBeRated(t *testing.T) {
	for _, status := range []trip.Status{
		trip.StatusRequested, trip.StatusAccepted, trip.StatusInProgress, trip.StatusCancelled,
	} {
		completedAt := time.Now().Add(-time.Minute)
		repo := &ratingRepoFake{found: trip.Trip{
			ID: ratedTripID, RiderID: ratedRider, DriverID: ratedDriver,
			Status: status, CompletedAt: &completedAt,
		}}

		_, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, "")
		if !errors.Is(err, trip.ErrTripNotRatable) {
			t.Errorf("status %q: got %v, want ErrTripNotRatable", status, err)
		}

		if len(repo.created) != 0 {
			t.Errorf("status %q: a rating was written", status)
		}
	}
}

func TestRateTrip_TheWindowIsTwentyFourHours(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want error
	}{
		{"just completed", time.Minute, nil},
		{"twenty-three hours ago", 23 * time.Hour, nil},
		{"twenty-five hours ago", 25 * time.Hour, trip.ErrRatingWindowClosed},
		{"a week ago", 7 * 24 * time.Hour, trip.ErrRatingWindowClosed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &ratingRepoFake{found: completedTripAgo(tc.age)}

			_, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("a completed trip with no completion time is treated as closed", func(t *testing.T) {
		repo := &ratingRepoFake{found: trip.Trip{
			ID: ratedTripID, RiderID: ratedRider, DriverID: ratedDriver, Status: trip.StatusCompleted,
		}}

		if _, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, ""); !errors.Is(err, trip.ErrRatingWindowClosed) {
			t.Fatalf("got %v, want ErrRatingWindowClosed", err)
		}
	})
}

func TestRateTrip_ATripWithoutBothSidesCannotBeRated(t *testing.T) {
	trip1 := completedTripAgo(time.Hour)
	trip1.DriverID = ""

	repo := &ratingRepoFake{found: trip1}

	if _, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, ""); !errors.Is(err, trip.ErrTripNotRatable) {
		t.Fatalf("got %v, want ErrTripNotRatable", err)
	}
}

func TestRateTrip_RepositoryOutcomesReachTheCaller(t *testing.T) {
	t.Run("a second rating by the same side", func(t *testing.T) {
		repo := &ratingRepoFake{
			found:    completedTripAgo(time.Hour),
			createFn: func(trip.CreateRatingInput) error { return trip.ErrAlreadyRated },
		}

		if _, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, ""); !errors.Is(err, trip.ErrAlreadyRated) {
			t.Fatalf("got %v, want ErrAlreadyRated", err)
		}
	})

	t.Run("a trip that does not exist", func(t *testing.T) {
		repo := &ratingRepoFake{findErr: trip.ErrTripNotFound}

		if _, err := rateAs(newRatingService(repo), trip.RatedByRider, 5, ""); !errors.Is(err, trip.ErrTripNotFound) {
			t.Fatalf("got %v, want ErrTripNotFound", err)
		}
	})
}
