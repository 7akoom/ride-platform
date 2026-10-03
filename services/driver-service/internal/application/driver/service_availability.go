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

	target := input.AvailabilityStatus

	// Only offline is allowed until an operator approves the driver. Reading
	// the status first is safe without a lock: a driver can never move back
	// to pending or rejected from active, so an active reading cannot go stale
	// in the wrong direction.
	if target != AvailabilityOffline {
		current, err := s.repository.FindByID(ctx, driverID)
		if err != nil {
			return Driver{}, fmt.Errorf("find driver: %w", err)
		}

		if !current.Status.CanGoOnline() {
			return Driver{}, ErrDriverNotApproved
		}

		// Taking trips needs every required document approved and in date.
		// A driver released from a trip (busy to available) whose document
		// ran out meanwhile ends up offline rather than stuck busy.
		if target == AvailabilityAvailable {
			compliance, err := s.compliance.CheckCompliance(ctx, driverID, ForWork)
			if err != nil {
				return Driver{}, fmt.Errorf("check driver documents: %w", err)
			}

			if !compliance.Compliant {
				if current.AvailabilityStatus != AvailabilityBusy {
					return Driver{}, ErrDocumentsIncomplete
				}

				target = AvailabilityOffline
			}
		}
	}

	updated, err := s.repository.UpdateAvailability(
		ctx,
		UpdateAvailabilityInput{
			DriverID:           driverID,
			AvailabilityStatus: target,
		},
	)
	if err != nil {
		return Driver{}, fmt.Errorf("update driver availability: %w", err)
	}

	return updated, nil
}
