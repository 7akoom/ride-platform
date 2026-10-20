package trip_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestRateTrip_TagsAreKeptOnceInTheirOrder(t *testing.T) {
	repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}
	svc := newRatingService(repo)

	_, err := svc.RateTrip(context.Background(), trip.RateTripInput{
		TripID:  ratedTripID,
		RatedBy: trip.RatedByRider,
		Stars:   5,
		Tags:    []string{trip.TagFriendly, trip.TagSafeDriving, trip.TagFriendly},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := repo.created[0].Tags; !slices.Equal(got, []string{trip.TagSafeDriving, trip.TagFriendly}) {
		t.Fatalf("tags %v", got)
	}
}

func TestRateTrip_ASideCannotGiveTheOtherSidesTags(t *testing.T) {
	cases := map[string]struct {
		by  trip.RatedBy
		tag string
	}{
		"the rider, a tag about riders":    {trip.RatedByRider, trip.TagPolite},
		"the driver, a tag about captains": {trip.RatedByDriver, trip.TagCleanCar},
		"an unknown tag":                   {trip.RatedByRider, "unknown"},
	}

	for name, tc := range cases {
		repo := &ratingRepoFake{found: completedTripAgo(time.Hour)}

		_, err := newRatingService(repo).RateTrip(context.Background(), trip.RateTripInput{
			TripID: ratedTripID, RatedBy: tc.by, Stars: 4, Tags: []string{tc.tag},
		})
		if !errors.Is(err, trip.ErrInvalidRatingTag) || len(repo.created) != 0 {
			t.Fatalf("%s: err = %v, created %d", name, err, len(repo.created))
		}
	}
}
