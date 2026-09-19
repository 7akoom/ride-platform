package dispatch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

func driverOfClass(id, class string) dispatch.DriverInfo {
	d := eligibleDriver(id)
	d.VehicleClass = class

	return d
}

func TestService_DispatchTrip_ComfortTripSkipsNearerEconomyDriver(t *testing.T) {
	h := newHarness()
	h.trip.trip.VehicleClass = "comfort"
	h.location.candidates = []dispatch.NearbyDriver{
		{DriverID: "driver-economy", DistanceMeters: 300},
		{DriverID: "driver-comfort", DistanceMeters: 900},
	}
	h.driver.drivers["driver-economy"] = driverOfClass("driver-economy", "economy")
	h.driver.drivers["driver-comfort"] = driverOfClass("driver-comfort", "comfort")
	h.wallet.standingFor["driver-economy"] = goodStanding()
	h.wallet.standingFor["driver-comfort"] = goodStanding()

	result, err := h.service().DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DriverID != "driver-comfort" {
		t.Fatalf("expected the comfort driver, got %+v", result)
	}

	if h.trip.acceptedBy != "driver-comfort" {
		t.Fatalf("AcceptTrip called for %q, want driver-comfort", h.trip.acceptedBy)
	}
}

func TestService_DispatchTrip_EconomyTripSkipsComfortDriver(t *testing.T) {
	h := newHarness()
	h.trip.trip.VehicleClass = "economy"
	h.location.candidates = []dispatch.NearbyDriver{
		{DriverID: "driver-comfort", DistanceMeters: 300},
		{DriverID: "driver-economy", DistanceMeters: 900},
	}
	h.driver.drivers["driver-comfort"] = driverOfClass("driver-comfort", "comfort")
	h.driver.drivers["driver-economy"] = driverOfClass("driver-economy", "economy")
	h.wallet.standingFor["driver-comfort"] = goodStanding()
	h.wallet.standingFor["driver-economy"] = goodStanding()

	result, err := h.service().DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DriverID != "driver-economy" {
		t.Fatalf("expected the economy driver, got %+v", result)
	}
}

func TestService_DispatchTrip_EmptyClassOnBothSidesMatchesAsEconomy(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1", DistanceMeters: 500}}
	h.driver.drivers["driver-1"] = driverOfClass("driver-1", "")
	h.wallet.standingFor["driver-1"] = goodStanding()

	result, err := h.service().DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DriverID != "driver-1" {
		t.Fatalf("expected driver-1, got %+v", result)
	}
}

func TestService_DispatchTrip_EmptyDriverClassCountsAsEconomyNotComfort(t *testing.T) {
	h := newHarness()
	h.trip.trip.VehicleClass = "comfort"
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1", DistanceMeters: 500}}
	h.driver.drivers["driver-1"] = driverOfClass("driver-1", "")
	h.wallet.standingFor["driver-1"] = goodStanding()

	_, err := h.service().DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, dispatch.ErrNoDriversAvailable) {
		t.Fatalf("error = %v, want ErrNoDriversAvailable", err)
	}

	if h.trip.acceptedBy != "" {
		t.Fatalf("trip must not be accepted, got %q", h.trip.acceptedBy)
	}
}

func TestService_DispatchTrip_NoDriverOfRequestedClass(t *testing.T) {
	h := newHarness()
	h.trip.trip.VehicleClass = "comfort"
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-economy", DistanceMeters: 300}}
	h.driver.drivers["driver-economy"] = driverOfClass("driver-economy", "economy")
	h.wallet.standingFor["driver-economy"] = goodStanding()

	_, err := h.service().DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, dispatch.ErrNoDriversAvailable) {
		t.Fatalf("error = %v, want ErrNoDriversAvailable", err)
	}
}

func TestService_DispatchTrip_AsksLocationForThirtyCandidates(t *testing.T) {
	h := newHarness()
	h.location.candidates = []dispatch.NearbyDriver{{DriverID: "driver-1", DistanceMeters: 500}}
	h.driver.drivers["driver-1"] = eligibleDriver("driver-1")
	h.wallet.standingFor["driver-1"] = goodStanding()

	if _, err := h.service().DispatchTrip(context.Background(), "trip-1", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.location.calledLimit != 30 {
		t.Fatalf("candidate limit = %d, want 30", h.location.calledLimit)
	}
}
