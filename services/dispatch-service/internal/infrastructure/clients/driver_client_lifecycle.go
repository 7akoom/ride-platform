package clients

import (
	"context"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
)

// MarkAvailable puts the driver back among those who can be offered a trip.
func (c *DriverClient) MarkAvailable(ctx context.Context, driverID string) error {
	_, err := c.client.UpdateAvailability(ctx, &driverv1.UpdateAvailabilityRequest{
		DriverId:           driverID,
		AvailabilityStatus: driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE,
	})
	if err != nil {
		return fmt.Errorf("call driver-service UpdateAvailability: %w", err)
	}

	return nil
}
