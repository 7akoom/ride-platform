package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixedOfferDuration struct {
	duration time.Duration
	err      error
}

func (f fixedOfferDuration) OfferDuration(context.Context) (time.Duration, error) {
	return f.duration, f.err
}

func offeredFor(t *testing.T, instance time.Duration, options ...Option) []time.Duration {
	t.Helper()

	trips := &offersTrips{info: TripInfo{ID: "trip-1", Status: "requested", VehicleClass: "economy"}, offerResults: map[string]error{}}
	options = append([]Option{WithOffers(instance)}, options...)
	service := NewService(trips, offersLocation{drivers: []string{"d1"}}, &offersDrivers{info: twoEligible()}, offersWallet{}, options...)

	if _, err := service.DispatchTrip(context.Background(), "trip-1", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return trips.offeredTTL
}

func TestTheOfferTimeStaffSetIsUsed(t *testing.T) {
	got := offeredFor(t, 15*time.Second, WithOfferDurations(fixedOfferDuration{duration: 25 * time.Second}))

	if len(got) != 1 || got[0] != 25*time.Second {
		t.Fatalf("want one offer for the 25s staff set, got %v", got)
	}
}

func TestWithoutAUsableSettingTheInstancesOfferTimeIsUsed(t *testing.T) {
	cases := map[string]OfferDurationSource{
		"left to the instance (0)":  fixedOfferDuration{},
		"the settings unreadable":   fixedOfferDuration{err: errors.New("pricing-service down")},
		"shorter than five seconds": fixedOfferDuration{duration: 2 * time.Second},
		"longer than a minute":      fixedOfferDuration{duration: 2 * time.Minute},
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			got := offeredFor(t, 15*time.Second, WithOfferDurations(source))

			if len(got) != 1 || got[0] != 15*time.Second {
				t.Fatalf("want one offer for the instance's 15s, got %v", got)
			}
		})
	}
}

func TestTheOfferTimeSettingNeverTurnsOffersOn(t *testing.T) {
	trips := &offersTrips{info: TripInfo{ID: "trip-1", Status: "requested", VehicleClass: "economy"}, offerResults: map[string]error{}}
	service := NewService(trips, offersLocation{drivers: []string{"d1"}}, &offersDrivers{info: twoEligible()}, offersWallet{},
		WithOffers(0), WithOfferDurations(fixedOfferDuration{duration: 25 * time.Second}))

	result, err := service.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offered || len(trips.offered) != 0 || len(trips.acceptedCalls) != 1 {
		t.Fatalf("an instance that assigns directly still does: %+v offered=%v accepted=%v", result, trips.offered, trips.acceptedCalls)
	}
}
