package clients

import (
	"context"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
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
) (events.DriverInfo, error) {
	response, err := c.client.GetDriver(ctx, &driverv1.GetDriverRequest{DriverId: driverID})
	if err != nil {
		return events.DriverInfo{}, fmt.Errorf("call driver-service GetDriver: %w", err)
	}

	driver := response.GetDriver()
	vehicle := driver.GetVehicle()

	return events.DriverInfo{
		ID:           driver.GetId(),
		DisplayName:  driver.GetDisplayName(),
		VehicleMake:  vehicle.GetMake(),
		VehicleModel: vehicle.GetModel(),
		VehicleColor: vehicle.GetColor(),
		PlateNumber:  vehicle.GetPlateNumber(),
	}, nil
}
