package trip

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) CancelTrip(
	ctx context.Context,
	tripID string,
	reason string,
) (Trip, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return Trip{}, ErrTripIDRequired
	}

	cancelled, err := s.repository.Cancel(ctx, trimmedID, strings.TrimSpace(reason))
	if err != nil {
		return Trip{}, fmt.Errorf("cancel trip: %w", err)
	}

	return cancelled, nil
}
