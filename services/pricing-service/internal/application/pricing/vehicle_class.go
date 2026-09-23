package pricing

import (
	"errors"
	"strings"
)

// Vehicle classes a fare can be priced for. They mirror the classes
// driver-service accepts; adding one means adding a constant here and
// widening pricing_configs_vehicle_class_check in a migration.
const (
	VehicleClassEconomy = "economy"
	VehicleClassComfort = "comfort"
)

var ErrInvalidVehicleClass = errors.New("invalid vehicle class")

// NormalizeVehicleClass lowercases and trims the class a caller asked for.
// An empty class means the caller did not specify one (clients that
// predate vehicle classes) and is priced as economy. Anything else that
// isn't a known class is rejected.
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

// VehicleClasses lists every class a trip can be priced for.
func VehicleClasses() []string {
	return []string{VehicleClassEconomy, VehicleClassComfort}
}

// effectiveClass maps a driver with no class on file to economy, the same
// way dispatch does.
func effectiveClass(class string) string {
	if normalized, err := NormalizeVehicleClass(class); err == nil {
		return normalized
	}

	return class
}
