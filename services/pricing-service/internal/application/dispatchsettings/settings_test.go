package dispatchsettings

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	saved []Settings
}

func (f *fakeStore) GetDispatchSettings(context.Context) (Settings, error) { return Defaults(), nil }

func (f *fakeStore) SetDispatchSettings(_ context.Context, s Settings) (Settings, error) {
	f.saved = append(f.saved, s)

	return s, nil
}

func TestTheDefaultsTakeNextTripsWithinThreeKilometres(t *testing.T) {
	got, err := NewService(&fakeStore{}).Get(context.Background())
	if err != nil || !got.NextTripsEnabled || got.NextTripRadiusMeters != 3000 {
		t.Fatalf("defaults %+v, %v", got, err)
	}
}

func TestTheRadiusIsBounded(t *testing.T) {
	for _, radius := range []int{0, 499, 10001} {
		store := &fakeStore{}

		if _, err := NewService(store).Update(context.Background(), settings(true, radius, 0)); !errors.Is(err, ErrInvalidRadius) || len(store.saved) != 0 {
			t.Errorf("%d: %v", radius, err)
		}
	}

	store := &fakeStore{}

	got, err := NewService(store).Update(context.Background(), settings(false, 1500, 0))
	if err != nil || got.NextTripsEnabled || got.NextTripRadiusMeters != 1500 || got.UpdatedBy != "staff-1" {
		t.Fatalf("saved %+v, %v", got, err)
	}
}

func TestTheOfferTimeIsTheDefaultOrBounded(t *testing.T) {
	for _, seconds := range []int{-1, 1, 4, 61, 300} {
		store := &fakeStore{}

		if _, err := NewService(store).Update(context.Background(), settings(true, 3000, seconds)); !errors.Is(err, ErrInvalidOfferSeconds) || len(store.saved) != 0 {
			t.Errorf("%d: %v", seconds, err)
		}
	}

	for _, seconds := range []int{0, 5, 20, 60} {
		got, err := NewService(&fakeStore{}).Update(context.Background(), settings(true, 3000, seconds))
		if err != nil || got.OfferSeconds != seconds {
			t.Errorf("%d: saved %+v, %v", seconds, got, err)
		}
	}
}

func settings(enabled bool, radius int, offerSeconds int) Settings {
	return Settings{NextTripsEnabled: enabled, NextTripRadiusMeters: radius, OfferSeconds: offerSeconds, UpdatedBy: " staff-1 "}
}
