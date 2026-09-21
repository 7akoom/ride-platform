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

	// Only offline is allowed until an operator approves the driver. Reading
	// the status first is safe without a lock: a driver can never move back
	// to pending or rejected from active, so an active reading cannot go stale
	// in the wrong direction.
	if input.AvailabilityStatus != AvailabilityOffline {
		current, err := s.repository.FindByID(ctx, driverID)
		if err != nil {
			return Driver{}, fmt.Errorf("find driver: %w", err)
		}

		if !current.Status.CanGoOnline() {
			return Driver{}, ErrDriverNotApproved
		}
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
