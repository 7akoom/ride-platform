package trip

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *service) TriggerSOS(
	ctx context.Context,
	tripID string,
	triggeredBy SosTriggeredBy,
	latitude, longitude float64,
) (string, time.Time, error) {
	trimmedID := strings.TrimSpace(tripID)
	if trimmedID == "" {
		return "", time.Time{}, ErrTripIDRequired
	}

	if !triggeredBy.Valid() {
		return "", time.Time{}, ErrInvalidSosTriggeredBy
	}

	location, err := NewCoordinates(latitude, longitude)
	if err != nil {
		return "", time.Time{}, err
	}

	alertID, triggeredAt, err := s.repository.TriggerSOS(ctx, trimmedID, triggeredBy, location)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("trigger sos: %w", err)
	}

	return alertID, triggeredAt, nil
}
