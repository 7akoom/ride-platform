package clients

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"google.golang.org/grpc"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const (
	// nearbyLimit is how many of the nearest drivers are looked at. Enough
	// to tell a busy area from a quiet one and to find each class's nearest.
	nearbyLimit = 15

	// driverLookupTimeout bounds each driver-service call; the lookups run
	// at the same time, so a quote waits for at most one of them.
	driverLookupTimeout = 1500 * time.Millisecond
)

// DriverFinder finds the free drivers near a point: location-service says
// who is near, driver-service which of them are active, available and in
// which class.
type DriverFinder struct {
	location *LocationClient
	drivers  driverv1.DriverServiceClient
}

func NewDriverFinder(location *LocationClient, driverConn grpc.ClientConnInterface) *DriverFinder {
	if location == nil {
		panic("location client is required")
	}

	if driverConn == nil {
		panic("driver-service connection is required")
	}

	return &DriverFinder{location: location, drivers: driverv1.NewDriverServiceClient(driverConn)}
}

// AvailableDriversNear returns the free drivers near a point, nearest
// first. A driver who could not be looked up is left out; if none of the
// nearby drivers could be, the answer is an error, not "no drivers": an
// unreachable driver-service must not look like an empty street (which
// would raise prices).
func (f *DriverFinder) AvailableDriversNear(
	ctx context.Context,
	latitude, longitude, radiusMeters float64,
) ([]pricing.NearbyDriver, error) {
	nearby, err := f.location.nearbyDrivers(ctx, latitude, longitude, radiusMeters, nearbyLimit)
	if err != nil {
		return nil, err
	}

	type lookup struct {
		driver *driverv1.Driver
		err    error
	}

	results := make([]lookup, len(nearby))

	var wg sync.WaitGroup

	for i, entity := range nearby {
		wg.Add(1)

		go func() {
			defer wg.Done()

			callCtx, cancel := context.WithTimeout(ctx, driverLookupTimeout)
			defer cancel()

			response, err := f.drivers.GetDriver(callCtx, &driverv1.GetDriverRequest{DriverId: entity.GetEntityId()})
			results[i] = lookup{driver: response.GetDriver(), err: err}
		}()
	}

	wg.Wait()

	var (
		free     []pricing.NearbyDriver
		failures []error
	)

	for i, entity := range nearby {
		result := results[i]
		if result.err != nil {
			failures = append(failures, result.err)

			continue
		}

		driver := result.driver
		if driver.GetStatus() != driverv1.DriverStatus_DRIVER_STATUS_ACTIVE ||
			driver.GetAvailabilityStatus() != driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE {
			continue
		}

		free = append(free, pricing.NearbyDriver{
			DriverID:     driver.GetId(),
			VehicleClass: driver.GetVehicle().GetVehicleClass(),
			Location: pricing.Point{
				Latitude:  entity.GetCoordinates().GetLatitude(),
				Longitude: entity.GetCoordinates().GetLongitude(),
			},
			DistanceMeters: entity.GetDistanceMeters(),
		})
	}

	if len(nearby) > 0 && len(failures) == len(nearby) {
		return nil, fmt.Errorf("look up nearby drivers: %w", errors.Join(failures...))
	}

	return free, nil
}
