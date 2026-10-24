package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type riderDirectoryFake struct {
	profile trip.RiderProfile
	err     error
	asked   []string
}

func (d *riderDirectoryFake) RiderProfile(_ context.Context, riderID string) (trip.RiderProfile, error) {
	d.asked = append(d.asked, riderID)

	return d.profile, d.err
}

type riderSummarizer interface {
	GetTripRider(ctx context.Context, tripID string) (trip.RiderSummary, error)
}

func TestGetTripRider_GivesTheDriverTheFirstNameAndRating(t *testing.T) {
	directory := &riderDirectoryFake{profile: trip.RiderProfile{DisplayName: "  Sara  Kamal Aziz ", RatingAverage: 4.9, RatingCount: 31}}
	svc := trip.WithRiderProfile(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), directory)

	summarizer, ok := trip.As[riderSummarizer](svc)
	if !ok {
		t.Fatal("the decorator does not offer GetTripRider")
	}

	got, err := summarizer.GetTripRider(context.Background(), ratedTripID)
	if err != nil {
		t.Fatalf("GetTripRider: %v", err)
	}

	if got.FirstName != "Sara" || got.RatingAverage != 4.9 || got.RatingCount != 31 {
		t.Errorf("unexpected summary: %+v", got)
	}

	if len(directory.asked) != 1 || directory.asked[0] != ratedRider {
		t.Errorf("the directory was asked for %v, want the trip's rider %s", directory.asked, ratedRider)
	}
}

func TestGetTripRider_FailuresReachTheCaller(t *testing.T) {
	missing := trip.WithRiderProfile(newRatingService(&ratingRepoFake{findErr: trip.ErrTripNotFound}), &riderDirectoryFake{})
	summarizer, _ := trip.As[riderSummarizer](missing)

	if _, err := summarizer.GetTripRider(context.Background(), ratedTripID); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("got %v, want ErrTripNotFound", err)
	}

	unreadable := trip.WithRiderProfile(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}),
		&riderDirectoryFake{err: trip.ErrRiderProfileUnavailable})
	summarizer, _ = trip.As[riderSummarizer](unreadable)

	if _, err := summarizer.GetTripRider(context.Background(), ratedTripID); !errors.Is(err, trip.ErrRiderProfileUnavailable) {
		t.Fatalf("got %v, want ErrRiderProfileUnavailable", err)
	}
}

func TestFirstName(t *testing.T) {
	for name, want := range map[string]string{"": "", "   ": "", "Ahmed": "Ahmed", "أحمد علي": "أحمد", " Sara\tK ": "Sara"} {
		if got := trip.FirstName(name); got != want {
			t.Errorf("FirstName(%q) = %q, want %q", name, got, want)
		}
	}
}

type rateTermsFake struct {
	terms trip.RateTerms
	err   error
	asked [][2]string
}

func (f *rateTermsFake) RateTerms(_ context.Context, tripID, riderID string) (trip.RateTerms, error) {
	f.asked = append(f.asked, [2]string{tripID, riderID})

	return f.terms, f.err
}

type driverTermsReader interface {
	GetDriverTripTerms(ctx context.Context, tripID string) (trip.DriverTerms, error)
}

func TestGetDriverTripTerms_TimesCountFromTheArrival(t *testing.T) {
	arrived := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	found := tripWithDriver(ratedDriver)
	found.ArrivedAt = &arrived
	rate := &rateTermsFake{terms: trip.RateTerms{CurrencyCode: "IQD", FreeWaitingMinutes: 3, WaitingPerMinute: "0", NoShowWaitMinutes: 6, NoShowFee: "2000"}}

	reader, ok := trip.As[driverTermsReader](trip.WithDriverTerms(newRatingService(&ratingRepoFake{found: found}), rate))
	if !ok {
		t.Fatal("the decorator does not offer GetDriverTripTerms")
	}

	got, err := reader.GetDriverTripTerms(context.Background(), ratedTripID)
	if err != nil {
		t.Fatalf("GetDriverTripTerms: %v", err)
	}

	if got.FreeWaitingUntil == nil || !got.FreeWaitingUntil.Equal(arrived.Add(3*time.Minute)) {
		t.Errorf("free waiting until %v", got.FreeWaitingUntil)
	}

	if got.NoShowFrom == nil || !got.NoShowFrom.Equal(arrived.Add(6*time.Minute)) {
		t.Errorf("no-show from %v", got.NoShowFrom)
	}

	if got.NoShowFee != "2000" || got.CurrencyCode != "IQD" || got.ArrivalRadiusMeters != 200 {
		t.Errorf("unexpected terms: %+v", got)
	}

	if len(rate.asked) != 1 || rate.asked[0] != [2]string{ratedTripID, ratedRider} {
		t.Errorf("pricing was asked for %v, want the trip and its rider", rate.asked)
	}
}

func TestGetDriverTripTerms_BeforeTheArrivalNoTimesYet(t *testing.T) {
	rate := &rateTermsFake{terms: trip.RateTerms{FreeWaitingMinutes: 3, NoShowWaitMinutes: 6}}
	reader, _ := trip.As[driverTermsReader](trip.WithDriverTerms(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), rate))

	got, err := reader.GetDriverTripTerms(context.Background(), ratedTripID)
	if err != nil || got.FreeWaitingUntil != nil || got.NoShowFrom != nil || got.NoShowWaitMinutes != 6 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestGetDriverTripTerms_PricingFailuresReachTheCaller(t *testing.T) {
	rate := &rateTermsFake{err: trip.ErrDriverTermsUnavailable}
	reader, _ := trip.As[driverTermsReader](trip.WithDriverTerms(newRatingService(&ratingRepoFake{found: tripWithDriver(ratedDriver)}), rate))

	if _, err := reader.GetDriverTripTerms(context.Background(), ratedTripID); !errors.Is(err, trip.ErrDriverTermsUnavailable) {
		t.Fatalf("got %v, want ErrDriverTermsUnavailable", err)
	}
}
