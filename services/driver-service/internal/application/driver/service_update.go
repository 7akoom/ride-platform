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

	vehicle, err := NewVehicle(
		input.VehicleMake,
		input.VehicleModel,
		input.VehicleColor,
		input.VehiclePlate,
	)
	if err != nil {
		return Driver{}, err
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
