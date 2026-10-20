package dues

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type storeFake struct {
	policy Policy
	cutoff time.Time
	limit  int
	riders []OverdueRider
}

func (f *storeFake) DuesPolicy(context.Context) (Policy, error) { return f.policy, nil }

func (f *storeFake) SetDuesPolicy(_ context.Context, policy Policy) (Policy, error) {
	f.policy = policy

	return policy, nil
}

func (f *storeFake) ListOverdueRiders(_ context.Context, oldestBefore time.Time, limit int) ([]OverdueRider, error) {
	f.cutoff, f.limit = oldestBefore, limit

	return f.riders, nil
}

func TestOverdueRidersArePastTheGraceDays(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	store := &storeFake{
		policy: Policy{BlockTrips: true, GraceDays: 30},
		riders: []OverdueRider{{RiderID: "r1", Outstanding: decimal.NewFromInt(2000), OldestAt: oldest}},
	}
	svc := NewService(store)
	svc.now = func() time.Time { return now }

	riders, err := svc.Overdue(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if !store.cutoff.Equal(now.AddDate(0, 0, -30)) || store.limit != defaultOverdueLimit {
		t.Fatalf("asked before %v, limit %d", store.cutoff, store.limit)
	}

	if len(riders) != 1 || !riders[0].PayBy.Equal(oldest.AddDate(0, 0, 30)) {
		t.Fatalf("riders %+v", riders)
	}
}

func TestThePolicyKeepsItsGraceDaysInRange(t *testing.T) {
	svc := NewService(&storeFake{})

	for _, days := range []int{-1, 366} {
		if _, err := svc.SetPolicy(context.Background(), Policy{GraceDays: days}); !errors.Is(err, ErrInvalidGraceDays) {
			t.Fatalf("%d days: err = %v", days, err)
		}
	}

	got, err := svc.SetPolicy(context.Background(), Policy{BlockTrips: true, GraceDays: 30})
	if err != nil || got != (Policy{BlockTrips: true, GraceDays: 30}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}
