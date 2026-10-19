package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestTripRating_AGivenRatingShowsItsStars(t *testing.T) {
	repo := &ratingRepoFake{found: completedTripAgo(time.Hour), rated: true, rating: trip.Rating{Stars: 4}}

	got, err := newRatingService(repo).TripRating(context.Background(), ratedTripID, trip.RatedByRider)
	if err != nil {
		t.Fatalf("TripRating: %v", err)
	}

	if !got.Rated || got.Stars != 4 || got.CanRate || got.CanRateUntil != nil {
		t.Fatalf("got %+v, want rated with 4 stars and nothing more to give", got)
	}
}

func TestTripRating_NotRatedInTimeCanStillBeRated(t *testing.T) {
	repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}

	got, err := newRatingService(repo).TripRating(context.Background(), ratedTripID, trip.RatedByRider)
	if err != nil {
		t.Fatalf("TripRating: %v", err)
	}

	want := repo.found.CompletedAt.Add(trip.RatingWindow)
	if got.Rated || !got.CanRate || got.CanRateUntil == nil || !got.CanRateUntil.Equal(want) {
		t.Fatalf("got %+v, want ratable until %v", got, want)
	}
}

func TestTripRating_TooLateOrNotCompletedCannotBeRated(t *testing.T) {
	late := &ratingRepoFake{found: completedTripAgo(trip.RatingWindow + time.Minute)}
	cancelled := &ratingRepoFake{found: trip.Trip{ID: ratedTripID, RiderID: ratedRider, Status: trip.StatusCancelled}}

	for name, repo := range map[string]*ratingRepoFake{"too late": late, "cancelled": cancelled} {
		got, err := newRatingService(repo).TripRating(context.Background(), ratedTripID, trip.RatedByRider)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if got.Rated || got.CanRate {
			t.Errorf("%s: got %+v, want neither rated nor ratable", name, got)
		}
	}
}

func TestTripRating_ChecksItsInput(t *testing.T) {
	svc := newRatingService(&ratingRepoFake{found: completedTripAgo(time.Hour)})

	if _, err := svc.TripRating(context.Background(), " ", trip.RatedByRider); !errors.Is(err, trip.ErrTripIDRequired) {
		t.Errorf("no trip: got %v", err)
	}

	if _, err := svc.TripRating(context.Background(), ratedTripID, ""); !errors.Is(err, trip.ErrInvalidRatedBy) {
		t.Errorf("no side: got %v", err)
	}
}
