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

		if _, err := NewService(store).Update(context.Background(), true, radius, "staff-1"); !errors.Is(err, ErrInvalidRadius) || len(store.saved) != 0 {
			t.Errorf("%d: %v", radius, err)
		}
	}

	store := &fakeStore{}

	got, err := NewService(store).Update(context.Background(), false, 1500, " staff-1 ")
	if err != nil || got.NextTripsEnabled || got.NextTripRadiusMeters != 1500 || got.UpdatedBy != "staff-1" {
		t.Fatalf("saved %+v, %v", got, err)
	}
}
