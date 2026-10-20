// Package dues is the staff side of what riders owe: the policy (whether
// unpaid fees block new trips, and after how many days) and the riders past it.
package dues

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

const (
	MaxGraceDays = 365

	defaultOverdueLimit = 50
	maxOverdueLimit     = 200
)

var ErrInvalidGraceDays = errors.New("grace days must be between 0 and 365")

// Policy is whether a rider who owes fees may not request trips once the
// oldest has been owed GraceDays.
type Policy struct {
	BlockTrips bool
	GraceDays  int
}

// OverdueRider owes fees whose oldest is past the grace days.
type OverdueRider struct {
	RiderID      string
	CurrencyCode string
	Outstanding  decimal.Decimal
	OldestAt     time.Time
	// PayBy is when the grace days ran out.
	PayBy time.Time
}

// Store is the wallet's database.
type Store interface {
	DuesPolicy(ctx context.Context) (Policy, error)
	SetDuesPolicy(ctx context.Context, policy Policy) (Policy, error)
	// ListOverdueRiders: riders whose oldest unpaid fee is from before
	// oldestBefore, the longest owed first.
	ListOverdueRiders(ctx context.Context, oldestBefore time.Time, limit int) ([]OverdueRider, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	if store == nil {
		panic("dues store is required")
	}

	return &Service{store: store, now: time.Now}
}

func (s *Service) Policy(ctx context.Context) (Policy, error) {
	return s.store.DuesPolicy(ctx)
}

func (s *Service) SetPolicy(ctx context.Context, policy Policy) (Policy, error) {
	if policy.GraceDays < 0 || policy.GraceDays > MaxGraceDays {
		return Policy{}, ErrInvalidGraceDays
	}

	return s.store.SetDuesPolicy(ctx, policy)
}

// Overdue lists the riders past the grace days, the longest owed first, for
// staff to follow up (and suspend when they decide to).
func (s *Service) Overdue(ctx context.Context, limit int) ([]OverdueRider, error) {
	switch {
	case limit <= 0:
		limit = defaultOverdueLimit
	case limit > maxOverdueLimit:
		limit = maxOverdueLimit
	}

	policy, err := s.store.DuesPolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the dues policy: %w", err)
	}

	riders, err := s.store.ListOverdueRiders(ctx, s.now().AddDate(0, 0, -policy.GraceDays), limit)
	if err != nil {
		return nil, fmt.Errorf("list overdue riders: %w", err)
	}

	for i := range riders {
		riders[i].PayBy = riders[i].OldestAt.AddDate(0, 0, policy.GraceDays)
	}

	return riders, nil
}
