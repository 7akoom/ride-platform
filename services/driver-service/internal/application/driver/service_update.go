package driver

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) UpdateDriverProfile(
	ctx context.Context,
	input UpdateDriverProfileInput,
) (Driver, error) {
	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return Driver{}, ErrDriverIDRequired
	}

	displayName, err := NewDisplayName(input.DisplayName)
	if err != nil {
		return Driver{}, err
	}

	// An empty VehicleClass is passed through as-is: the repository reads
	// it as "leave the stored class unchanged", so editing a name or plate
	// never silently downgrades a comfort driver.
	vehicle, err := NewVehicle(
		input.VehicleMake,
		input.VehicleModel,
		input.VehicleColor,
		input.VehiclePlate,
		input.VehicleClass,
	)
	if err != nil {
		return Driver{}, err
	}

	current, err := s.repository.FindByID(ctx, driverID)
	if err != nil {
		return Driver{}, fmt.Errorf("find driver: %w", err)
	}

	// Riders are shown the approved name and car; once approved they change
	// only through staff. Sending them unchanged is fine.
	if current.Status == StatusActive || current.Status == StatusSuspended {
		if displayName.String() != current.DisplayName || !sameVehicle(vehicle, current.Vehicle) {
			return Driver{}, ErrProfileLocked
		}
	}

	updated, err := s.repository.UpdateProfile(
		ctx,
		UpdateProfileInput{
			DriverID:    driverID,
			DisplayName: displayName.String(),
			Vehicle:     vehicle,
		},
	)
	if err != nil {
		return Driver{}, fmt.Errorf("update driver profile: %w", err)
	}

	return updated, nil
}

// sameVehicle compares a requested vehicle with the stored one; an empty
// requested class means "unchanged".
func sameVehicle(requested, stored Vehicle) bool {
	return requested.Make == stored.Make &&
		requested.Model == stored.Model &&
		requested.Color == stored.Color &&
		requested.PlateNumber == stored.PlateNumber &&
		(requested.Class == "" || requested.Class == stored.Class)
}
