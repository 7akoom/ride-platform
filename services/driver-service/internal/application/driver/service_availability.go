package driver

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) UpdateAvailability(
	ctx context.Context,
	input UpdateDriverAvailabilityInput,
) (Driver, error) {
	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return Driver{}, ErrDriverIDRequired
	}

	if !input.AvailabilityStatus.Valid() {
		return Driver{}, ErrInvalidAvailability
	}

	updated, err := s.repository.UpdateAvailability(
		ctx,
		UpdateAvailabilityInput{
			DriverID:           driverID,
			AvailabilityStatus: input.AvailabilityStatus,
		},
	)
	if err != nil {
		return Driver{}, fmt.Errorf("update driver availability: %w", err)
	}

	return updated, nil
}
