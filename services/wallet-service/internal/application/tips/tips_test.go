package tips

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	riderID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	tripID  = "11111111-1111-4111-8111-111111111111"
)

type fakeStore struct {
	byKey     map[string]Tip
	given     int
	notBefore time.Time
}

func (f *fakeStore) Config(context.Context) (wallet.Config, error) {
	return wallet.Config{TipMinAmount: decimal.NewFromInt(250), TipMaxAmount: decimal.NewFromInt(25000)}, nil
}

func (f *fakeStore) FindByKey(_ context.Context, rider, key string) (Tip, bool, error) {
	t, ok := f.byKey[rider+"/"+key]

	return t, ok, nil
}

func (f *fakeStore) Tip(_ context.Context, r Record, notBefore time.Time) (Tip, wallet.Wallet, error) {
	f.given++
	f.notBefore = notBefore
	t := Tip{ID: "tip-1", TripID: r.TripID, RiderID: r.RiderID, Amount: r.Amount}
	f.byKey[r.RiderID+"/"+r.IdempotencyKey] = t

	return t, wallet.Wallet{ID: "w1"}, nil
}

func TestATipIsCheckedAndGivenOnce(t *testing.T) {
	store := &fakeStore{byKey: map[string]Tip{}}
	s := NewService(store)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := context.Background()

	valid := Input{RiderID: riderID, TripID: tripID, Amount: decimal.NewFromInt(1000), IdempotencyKey: "k1"}

	for name, c := range map[string]struct {
		edit func(*Input)
		want error
	}{
		"no trip":         {func(in *Input) { in.TripID = "trip-1" }, ErrTripRequired},
		"no rider":        {func(in *Input) { in.RiderID = "" }, ErrTripRequired},
		"zero":            {func(in *Input) { in.Amount = decimal.Zero }, ErrInvalidAmount},
		"no key":          {func(in *Input) { in.IdempotencyKey = "" }, ErrIdempotencyKey},
		"below the least": {func(in *Input) { in.Amount = decimal.NewFromInt(100) }, ErrBelowMin},
		"above the most":  {func(in *Input) { in.Amount = decimal.NewFromInt(25001) }, ErrAboveMax},
	} {
		input := valid
		c.edit(&input)

		if _, _, err := s.Give(ctx, input); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	if _, balance, err := s.Give(ctx, valid); err != nil || balance.ID != "w1" || !store.notBefore.Equal(now.Add(-Window)) {
		t.Fatalf("give %+v %v (not before %v)", balance, err, store.notBefore)
	}

	if again, balance, err := s.Give(ctx, valid); err != nil || again.ID != "tip-1" || balance.ID != "" || store.given != 1 {
		t.Fatalf("again %+v %v (given %d)", again, err, store.given)
	}

	other := valid
	other.Amount = decimal.NewFromInt(2000)

	if _, _, err := s.Give(ctx, other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key for another tip: %v", err)
	}
}
