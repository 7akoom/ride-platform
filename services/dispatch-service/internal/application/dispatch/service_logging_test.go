package dispatch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type logTestTrip struct {
	info      TripInfo
	acceptErr error
}

func (f *logTestTrip) GetTrip(_ context.Context, _ string) (TripInfo, error) {
	return f.info, nil
}

func (f *logTestTrip) AcceptTrip(_ context.Context, _ string, _ string) error {
	return f.acceptErr
}

type logTestLocation struct {
	drivers []NearbyDriver
}

func (f *logTestLocation) FindNearbyDrivers(
	_ context.Context,
	_, _, _ float64,
	_ int,
) ([]NearbyDriver, error) {
	return f.drivers, nil
}

type logTestDriver struct {
	info DriverInfo
}

func (f *logTestDriver) GetDriver(_ context.Context, driverID string) (DriverInfo, error) {
	info := f.info
	info.ID = driverID

	return info, nil
}

func (f *logTestDriver) MarkBusy(_ context.Context, _ string) error {
	return nil
}

type logTestWallet struct {
	standing DriverStanding
}

func (f *logTestWallet) CheckDriverStanding(_ context.Context, _ string) (DriverStanding, error) {
	return f.standing, nil
}

func newLogTestService(
	trip *logTestTrip,
	location *logTestLocation,
	driver *logTestDriver,
	wallet *logTestWallet,
) (Service, *bytes.Buffer) {
	var buffer bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&buffer, nil))

	return NewService(trip, location, driver, wallet, WithLogger(logger)), &buffer
}

func logTestEligibleDriver() *logTestDriver {
	return &logTestDriver{info: DriverInfo{
		Status:             driverStatusActive,
		AvailabilityStatus: driverAvailable,
		VehicleClass:       "economy",
	}}
}

func logTestRequestedTrip() *logTestTrip {
	return &logTestTrip{info: TripInfo{ID: "trip-1", Status: statusRequested, VehicleClass: "economy"}}
}

func logTestOneNearby() *logTestLocation {
	return &logTestLocation{drivers: []NearbyDriver{{DriverID: "driver-1", DistanceMeters: 50}}}
}

func TestLogsWhenNoDriversNearby(t *testing.T) {
	service, buffer := newLogTestService(
		logTestRequestedTrip(), &logTestLocation{}, logTestEligibleDriver(),
		&logTestWallet{standing: DriverStanding{CanTakeTrips: true}},
	)

	_, err := service.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, ErrNoDriversNearby) {
		t.Fatalf("expected ErrNoDriversNearby, got %v", err)
	}

	if !strings.Contains(buffer.String(), "no drivers found near the pickup point") {
		t.Fatalf("expected an explanatory log line, got:\n%s", buffer.String())
	}
}

func TestLogsSuspendedWalletAsSkipReason(t *testing.T) {
	service, buffer := newLogTestService(
		logTestRequestedTrip(), logTestOneNearby(), logTestEligibleDriver(),
		&logTestWallet{standing: DriverStanding{CanTakeTrips: false, Suspended: true, Reason: "deposit required"}},
	)

	_, err := service.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, ErrNoDriversAvailable) {
		t.Fatalf("expected ErrNoDriversAvailable, got %v", err)
	}

	logged := buffer.String()

	for _, want := range []string{"driver-1", "wallet does not allow trips", "deposit required"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log is missing %q:\n%s", want, logged)
		}
	}
}

func TestLogsAcceptFailureAsSkipReason(t *testing.T) {
	trip := logTestRequestedTrip()
	trip.acceptErr = errors.New("driver already has an active trip")

	service, buffer := newLogTestService(
		trip, logTestOneNearby(), logTestEligibleDriver(),
		&logTestWallet{standing: DriverStanding{CanTakeTrips: true}},
	)

	_, err := service.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, ErrNoDriversAvailable) {
		t.Fatalf("expected ErrNoDriversAvailable, got %v", err)
	}

	if !strings.Contains(buffer.String(), "accept trip failed: driver already has an active trip") {
		t.Fatalf("log is missing the accept failure reason:\n%s", buffer.String())
	}
}

func TestLogsVehicleClassMismatchAsSkipReason(t *testing.T) {
	trip := logTestRequestedTrip()
	trip.info.VehicleClass = "comfort"

	service, buffer := newLogTestService(
		trip, logTestOneNearby(), logTestEligibleDriver(),
		&logTestWallet{standing: DriverStanding{CanTakeTrips: true}},
	)

	_, err := service.DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, ErrNoDriversAvailable) {
		t.Fatalf("expected ErrNoDriversAvailable, got %v", err)
	}

	if !strings.Contains(buffer.String(), "vehicle class mismatch (driver=economy, trip=comfort)") {
		t.Fatalf("log is missing the class mismatch reason:\n%s", buffer.String())
	}
}

func TestSuccessfulDispatchLogsNoFailureLine(t *testing.T) {
	service, buffer := newLogTestService(
		logTestRequestedTrip(), logTestOneNearby(), logTestEligibleDriver(),
		&logTestWallet{standing: DriverStanding{CanTakeTrips: true}},
	)

	result, err := service.DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if result.DriverID != "driver-1" {
		t.Fatalf("expected driver-1, got %q", result.DriverID)
	}

	if strings.Contains(buffer.String(), "none could be assigned") {
		t.Fatalf("a successful dispatch must not log a failure line:\n%s", buffer.String())
	}
}

func TestServiceWithoutLoggerStaysSilentAndWorks(t *testing.T) {
	service := NewService(
		logTestRequestedTrip(), &logTestLocation{}, logTestEligibleDriver(),
		&logTestWallet{standing: DriverStanding{CanTakeTrips: true}},
	)

	if _, err := service.DispatchTrip(context.Background(), "trip-1", 0); !errors.Is(err, ErrNoDriversNearby) {
		t.Fatalf("expected ErrNoDriversNearby, got %v", err)
	}
}
