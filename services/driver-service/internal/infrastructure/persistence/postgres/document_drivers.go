package postgres

import (
	"context"
	"errors"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// DocumentDrivers answers the documents service's questions about drivers
// from the drivers table.
type DocumentDrivers struct {
	drivers *DriverRepository
}

var _ documents.Drivers = (*DocumentDrivers)(nil)

func NewDocumentDrivers(drivers *DriverRepository) *DocumentDrivers {
	if drivers == nil {
		panic("driver repository is required")
	}

	return &DocumentDrivers{drivers: drivers}
}

func (d *DocumentDrivers) IdentityOf(ctx context.Context, driverID string) (string, error) {
	found, err := d.drivers.FindByID(ctx, driverID)
	if errors.Is(err, driver.ErrDriverNotFound) {
		return "", documents.ErrDriverNotFound
	}

	if err != nil {
		return "", err
	}

	return found.IdentityID, nil
}
