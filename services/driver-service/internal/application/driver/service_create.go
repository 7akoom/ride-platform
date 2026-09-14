package driver

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) CreateDriver(
	ctx context.Context,
	input CreateDriverInput,
) (Driver, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return Driver{}, ErrIdentityIDRequired
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

	existing, err := s.repository.FindByIdentityID(ctx, identityID)
	switch {
	case err == nil:
		_ = existing

		return Driver{}, ErrDriverAlreadyExists
	case errors.Is(err, ErrDriverNotFound):
		// Expected path: no driver yet for this identity.
	default:
		return Driver{}, fmt.Errorf("check existing driver for identity: %w", err)
	}

	created, err := s.repository.Create(
		ctx,
		CreateInput{
			ID:          s.idGenerator.NewID(),
			IdentityID:  identityID,
			DisplayName: displayName.String(),
			Vehicle:     vehicle,
		},
	)
	if err != nil {
		return Driver{}, fmt.Errorf("create driver: %w", err)
	}

	return created, nil
}
