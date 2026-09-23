package clients

import (
	"context"
	"errors"
	"testing"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeLocation struct {
	locationv1.LocationServiceClient

	nearby []*locationv1.NearbyEntity
	err    error
}

func (f *fakeLocation) FindNearby(context.Context, *locationv1.FindNearbyRequest, ...grpc.CallOption) (*locationv1.FindNearbyResponse, error) {
	return &locationv1.FindNearbyResponse{Entities: f.nearby}, f.err
}

func (f *fakeLocation) GetCity(_ context.Context, r *locationv1.GetCityRequest, _ ...grpc.CallOption) (*locationv1.CityResponse, error) {
	switch r.GetCityId() {
	case "known":
		return &locationv1.CityResponse{}, nil
	case "down":
		return nil, status.Error(codes.Unavailable, "down")
	default:
		return nil, status.Error(codes.NotFound, "no")
	}
}

type fakeDrivers struct {
	driverv1.DriverServiceClient

	drivers map[string]*driverv1.Driver
}

func (f *fakeDrivers) GetDriver(_ context.Context, r *driverv1.GetDriverRequest, _ ...grpc.CallOption) (*driverv1.GetDriverResponse, error) {
	d, ok := f.drivers[r.GetDriverId()]
	if !ok {
		return nil, status.Error(codes.Unavailable, "driver-service down")
	}

	return &driverv1.GetDriverResponse{Driver: d}, nil
}

func entity(id string, meters float64) *locationv1.NearbyEntity {
	return &locationv1.NearbyEntity{EntityId: id, DistanceMeters: meters, Coordinates: &locationv1.Coordinates{Latitude: 36.2, Longitude: 44.0}}
}

func driver(id, class string, st driverv1.DriverStatus, av driverv1.AvailabilityStatus) *driverv1.Driver {
	return &driverv1.Driver{Id: id, Status: st, AvailabilityStatus: av, Vehicle: &driverv1.Vehicle{VehicleClass: class}}
}

const (
	active    = driverv1.DriverStatus_DRIVER_STATUS_ACTIVE
	pending   = driverv1.DriverStatus_DRIVER_STATUS_PENDING
	available = driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE
	busy      = driverv1.AvailabilityStatus_AVAILABILITY_STATUS_BUSY
)

func TestOnlyFreeActiveDriversCountNearestFirst(t *testing.T) {
	location := &fakeLocation{nearby: []*locationv1.NearbyEntity{
		entity("a", 100), entity("busy", 200), entity("pending", 300), entity("c", 400), entity("gone", 500),
	}}
	drivers := &fakeDrivers{drivers: map[string]*driverv1.Driver{
		"a":       driver("a", "economy", active, available),
		"busy":    driver("busy", "economy", active, busy),
		"pending": driver("pending", "economy", pending, available),
		"c":       driver("c", "comfort", active, available),
	}}

	finder := &DriverFinder{location: &LocationClient{client: location}, drivers: drivers}

	got, err := finder.AvailableDriversNear(context.Background(), 36.19, 44.01, 5000)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].DriverID != "a" || got[1].DriverID != "c" || got[1].VehicleClass != "comfort" || got[0].Location.Latitude != 36.2 {
		t.Fatalf("got %+v", got)
	}
}

func TestDriversThatCannotBeLookedUpAreNotAnEmptyStreet(t *testing.T) {
	location := &fakeLocation{nearby: []*locationv1.NearbyEntity{entity("x", 100), entity("y", 200)}}
	finder := &DriverFinder{location: &LocationClient{client: location}, drivers: &fakeDrivers{}}

	if _, err := finder.AvailableDriversNear(context.Background(), 36.19, 44.01, 5000); err == nil {
		t.Fatal("every lookup failed: expected an error")
	}

	location.nearby = nil
	if got, err := finder.AvailableDriversNear(context.Background(), 36.19, 44.01, 5000); err != nil || len(got) != 0 {
		t.Fatalf("nobody near: %v %v", got, err)
	}

	location.err = errors.New("location down")
	if _, err := finder.AvailableDriversNear(context.Background(), 36.19, 44.01, 5000); err == nil {
		t.Fatal("location down: expected an error")
	}
}

func TestPlaceExistence(t *testing.T) {
	c := &LocationClient{client: &fakeLocation{}}

	if ok, err := c.CityExists(context.Background(), "known"); !ok || err != nil {
		t.Fatalf("known: %v %v", ok, err)
	}

	if ok, err := c.CityExists(context.Background(), "other"); ok || err != nil {
		t.Fatalf("unknown: %v %v", ok, err)
	}

	if _, err := c.CityExists(context.Background(), "down"); err == nil {
		t.Fatal("down: expected an error")
	}
}
