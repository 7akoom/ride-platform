package clients

import (
	"context"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DriverDirectory reads a driver's profile from driver-service. It calls with the internal
// service token: a rider may not read a driver's profile from driver-service directly, and
// trip-service has already checked that this rider is on this trip. Only the fields a rider
// may see are copied out.
type DriverDirectory struct {
	client driverv1.DriverServiceClient
}

func NewDriverDirectory(conn *grpc.ClientConn) *DriverDirectory {
	if conn == nil {
		panic("driver-service connection is required")
	}

	return &DriverDirectory{client: driverv1.NewDriverServiceClient(conn)}
}

func (d *DriverDirectory) DriverSummary(
	ctx context.Context,
	driverID string,
) (trip.DriverSummary, error) {
	response, err := d.client.GetDriver(ctx, &driverv1.GetDriverRequest{DriverId: driverID})
	if status.Code(err) == codes.NotFound {
		return trip.DriverSummary{}, trip.ErrDriverProfileUnavailable
	}

	if err != nil {
		return trip.DriverSummary{}, fmt.Errorf("call driver-service GetDriver: %w", err)
	}

	driver := response.GetDriver()
	vehicle := driver.GetVehicle()

	return trip.DriverSummary{
		DisplayName:   driver.GetDisplayName(),
		VehicleMake:   vehicle.GetMake(),
		VehicleModel:  vehicle.GetModel(),
		VehicleColor:  vehicle.GetColor(),
		PlateNumber:   vehicle.GetPlateNumber(),
		VehicleClass:  vehicle.GetVehicleClass(),
		RatingAverage: driver.GetRatingAverage(),
		RatingCount:   driver.GetRatingCount(),
	}, nil
}
