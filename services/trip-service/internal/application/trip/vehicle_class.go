package trip

import (
	"errors"
	"strings"
)

// Vehicle classes a rider can request. They mirror driver-service and
// pricing-service; adding one means adding a constant here, widening
// trips_vehicle_class_check in a migration, and updating the other two.
const (
	VehicleClassEconomy = "economy"
	VehicleClassComfort = "comfort"
)

var ErrInvalidVehicleClass = errors.New("invalid vehicle class")

// NormalizeVehicleClass lowercases and trims the requested class. Empty
// means the client did not specify one (it predates vehicle classes) and
// is treated as economy. Anything unknown is rejected.
func NormalizeVehicleClass(raw string) (string, error) {
	class := strings.ToLower(strings.TrimSpace(raw))

	switch class {
	case "":
		return VehicleClassEconomy, nil
	case VehicleClassEconomy, VehicleClassComfort:
		return class, nil
	default:
		return "", ErrInvalidVehicleClass
	}
}
