package driver

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) GetDriver(
	ctx context.Context,
	driverID string,
) (Driver, error) {
	trimmedID := strings.TrimSpace(driverID)
	if trimmedID == "" {
		return Driver{}, ErrDriverIDRequired
	}

	found, err := s.repository.FindByID(ctx, trimmedID)
	if err != nil {
		return Driver{}, fmt.Errorf("find driver by id: %w", err)
	}

	return found, nil
}

func (s *service) GetDriverByIdentityID(
	ctx context.Context,
	identityID string,
) (Driver, error) {
	trimmedID := strings.TrimSpace(identityID)
	if trimmedID == "" {
		return Driver{}, ErrIdentityIDRequired
	}

	found, err := s.repository.FindByIdentityID(ctx, trimmedID)
	if err != nil {
		return Driver{}, fmt.Errorf("find driver by identity id: %w", err)
	}

	return found, nil
}
