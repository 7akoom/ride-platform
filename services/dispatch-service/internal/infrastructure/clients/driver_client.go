package clients

import (
	"context"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc"
)

type DriverClient struct {
	client driverv1.DriverServiceClient
}

func NewDriverClient(conn *grpc.ClientConn) *DriverClient {
	if conn == nil {
		panic("driver-service connection is required")
	}

	return &DriverClient{client: driverv1.NewDriverServiceClient(conn)}
}

func (c *DriverClient) GetDriver(
	ctx context.Context,
	driverID string,
) (dispatch.DriverInfo, error) {
	response, err := c.client.GetDriver(ctx, &driverv1.GetDriverRequest{DriverId: driverID})
	if err != nil {
		return dispatch.DriverInfo{}, fmt.Errorf("call driver-service GetDriver: %w", err)
	}

	driver := response.GetDriver()

	return dispatch.DriverInfo{
		ID:                 driver.GetId(),
		Status:             driverStatusToString(driver.GetStatus()),
		AvailabilityStatus: availabilityToString(driver.GetAvailabilityStatus()),
		VehicleClass:       driver.GetVehicle().GetVehicleClass(),
	}, nil
}

func (c *DriverClient) MarkBusy(
	ctx context.Context,
	driverID string,
) error {
	_, err := c.client.UpdateAvailability(ctx, &driverv1.UpdateAvailabilityRequest{
		DriverId:           driverID,
		AvailabilityStatus: driverv1.AvailabilityStatus_AVAILABILITY_STATUS_BUSY,
	})
	if err != nil {
		return fmt.Errorf("call driver-service UpdateAvailability: %w", err)
	}

	return nil
}

// driverStatusToString names every status the driver-service can return.
// Dispatch only assigns "active" drivers; the other names exist so its log
// says "pending" for a driver awaiting approval instead of "unspecified".
func driverStatusToString(s driverv1.DriverStatus) string {
	switch s {
	case driverv1.DriverStatus_DRIVER_STATUS_ACTIVE:
		return "active"
	case driverv1.DriverStatus_DRIVER_STATUS_SUSPENDED:
		return "suspended"
	case driverv1.DriverStatus_DRIVER_STATUS_PENDING:
		return "pending"
	case driverv1.DriverStatus_DRIVER_STATUS_REJECTED:
		return "rejected"
	default:
		return "unspecified"
	}
}

func availabilityToString(a driverv1.AvailabilityStatus) string {
	switch a {
	case driverv1.AvailabilityStatus_AVAILABILITY_STATUS_OFFLINE:
		return "offline"
	case driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE:
		return "available"
	case driverv1.AvailabilityStatus_AVAILABILITY_STATUS_BUSY:
		return "busy"
	default:
		return "unspecified"
	}
}
