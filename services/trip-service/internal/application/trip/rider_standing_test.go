package trip_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type fakeStanding struct {
	standing trip.Standing
	err      error
	asked    []string
}

func (f *fakeStanding) RiderStanding(_ context.Context, riderID string) (trip.Standing, error) {
	f.asked = append(f.asked, riderID)

	return f.standing, f.err
}

func standingRig(standing *fakeStanding) (trip.Service, *fakeRepository) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}

	return trip.NewService(repo, &fakeIDGenerator{id: "new-trip-id"}, &fakeZoneChecker{served: true}, trip.WithRiderStanding(standing)), repo
}

func TestARiderWhoOwesFeesCannotRequestATrip(t *testing.T) {
	standing := &fakeStanding{standing: trip.Standing{CanRequestTrips: false, Outstanding: "1500", CurrencyCode: "IQD"}}
	svc, repo := standingRig(standing)

	_, err := svc.RequestTrip(context.Background(), validRequestInput())
	if !errors.Is(err, trip.ErrRiderOwesFees) {
		t.Fatalf("got %v", err)
	}

	if err.Error() != trip.ErrRiderOwesFees.Error()+" (1500 IQD)" {
		t.Fatalf("message %q", err.Error())
	}

	if len(repo.createCalls) != 0 {
		t.Fatal("a trip was created")
	}

	if len(standing.asked) != 1 || standing.asked[0] != "rider-1" {
		t.Fatalf("asked %v", standing.asked)
	}
}

func TestARiderInGoodStandingRequestsATrip(t *testing.T) {
	svc, repo := standingRig(&fakeStanding{standing: trip.Standing{CanRequestTrips: true, Outstanding: "1500", CurrencyCode: "IQD"}})

	if _, err := svc.RequestTrip(context.Background(), validRequestInput()); err != nil {
		t.Fatal(err)
	}

	if len(repo.createCalls) != 1 {
		t.Fatalf("created %d", len(repo.createCalls))
	}
}

func TestWhileTheWalletIsDownTripsAreNotBlocked(t *testing.T) {
	svc, repo := standingRig(&fakeStanding{err: errors.Join(trip.ErrUpstreamUnavailable, errors.New("connection refused"))})

	if _, err := svc.RequestTrip(context.Background(), validRequestInput()); err != nil {
		t.Fatal(err)
	}

	if len(repo.createCalls) != 1 {
		t.Fatalf("created %d", len(repo.createCalls))
	}
}

func TestAnUnexpectedStandingFailureIsAnError(t *testing.T) {
	svc, repo := standingRig(&fakeStanding{err: errors.New("boom")})

	if _, err := svc.RequestTrip(context.Background(), validRequestInput()); err == nil {
		t.Fatal("want an error")
	}

	if len(repo.createCalls) != 0 {
		t.Fatal("a trip was created")
	}
}

func TestTheStandingIsNotAskedForARiderWithAnActiveTrip(t *testing.T) {
	standing := &fakeStanding{standing: trip.Standing{CanRequestTrips: true}}
	svc, repo := standingRig(standing)
	repo.findActiveByRiderIDErr = nil

	if _, err := svc.RequestTrip(context.Background(), validRequestInput()); !errors.Is(err, trip.ErrRiderHasActiveTrip) {
		t.Fatalf("got %v", err)
	}

	if len(standing.asked) != 0 {
		t.Fatalf("asked %v", standing.asked)
	}
}
