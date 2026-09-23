package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultRecentDestinations = 5
	MaxRecentDestinations     = 10
)

// ErrInvalidLimit: a negative limit.
var ErrInvalidLimit = errors.New("limit must not be negative")

// Destination is where one or more of the rider's completed trips ended.
type Destination struct {
	Coordinates Coordinates
	Address     string
	LastTripAt  time.Time
}

// DestinationStore lists where a rider's completed trips ended, newest first,
// each place once (points within about 10 m are the same place).
type DestinationStore interface {
	RecentDestinations(ctx context.Context, riderID string, limit int) ([]Destination, error)
}

// WithRecentDestinations decorates a Service with RecentDestinations. Who may
// ask (the rider themselves) is decided before this is called.
func WithRecentDestinations(base Service, store DestinationStore) Service {
	if base == nil {
		panic("trip service is required")
	}

	if store == nil {
		panic("destination store is required")
	}

	return &recentDestinationService{Service: base, store: store}
}

type recentDestinationService struct {
	Service

	store DestinationStore
}

func (s *recentDestinationService) Unwrap() Service { return s.Service }

func (s *recentDestinationService) RecentDestinations(ctx context.Context, riderID string, limit int) ([]Destination, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return nil, ErrRiderIDRequired
	}

	switch {
	case limit < 0:
		return nil, ErrInvalidLimit
	case limit == 0:
		limit = DefaultRecentDestinations
	case limit > MaxRecentDestinations:
		limit = MaxRecentDestinations
	}

	found, err := s.store.RecentDestinations(ctx, riderID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent destinations: %w", err)
	}

	return found, nil
}
