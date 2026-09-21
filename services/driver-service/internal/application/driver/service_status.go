package driver

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) ApproveDriver(
	ctx context.Context,
	driverID string,
) (Driver, error) {
	return s.changeStatus(ctx, driverID, StatusActive, StatusPending, StatusRejected)
}

func (s *service) RejectDriver(
	ctx context.Context,
	driverID string,
) (Driver, error) {
	return s.changeStatus(ctx, driverID, StatusRejected, StatusPending)
}

func (s *service) changeStatus(
	ctx context.Context,
	driverID string,
	to Status,
	allowedFrom ...Status,
) (Driver, error) {
	trimmedID := strings.TrimSpace(driverID)
	if trimmedID == "" {
		return Driver{}, ErrDriverIDRequired
	}

	updated, err := s.repository.UpdateStatus(
		ctx,
		UpdateStatusInput{
			DriverID:    trimmedID,
			To:          to,
			AllowedFrom: allowedFrom,
		},
	)
	if err != nil {
		return Driver{}, fmt.Errorf("change driver status to %s: %w", to, err)
	}

	return updated, nil
}
