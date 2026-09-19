package dispatch

// defaultVehicleClass is what a trip or driver with no class recorded is
// treated as, matching driver-service, pricing-service and trip-service.
const defaultVehicleClass = "economy"

// effectiveVehicleClass maps an empty class to economy, so a driver and a
// trip are only compared on real values.
func effectiveVehicleClass(class string) string {
	if class == "" {
		return defaultVehicleClass
	}

	return class
}
