package trip

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) StartTrip(
	ctx context.Context,
	tripID string,
) (Trip, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return Trip{}, ErrTripIDRequired
	}

	started, err := s.repository.Start(ctx, trimmedID)
	if err != nil {
		return Trip{}, fmt.Errorf("start trip: %w", err)
	}

	return started, nil
}

func (s *service) CompleteTrip(
	ctx context.Context,
	tripID string,
) (Trip, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return Trip{}, ErrTripIDRequired
	}

	completed, err := s.repository.Complete(ctx, trimmedID)
	if err != nil {
		return Trip{}, fmt.Errorf("complete trip: %w", err)
	}

	return completed, nil
}

func (s *service) GetTrip(
	ctx context.Context,
	tripID string,
) (Trip, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return Trip{}, ErrTripIDRequired
	}

	found, err := s.repository.FindByID(ctx, trimmedID)
	if err != nil {
		return Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return found, nil
}
