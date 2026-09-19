package events

import "context"

// TripInfo is the handler's view of a trip — just enough to resolve the
// rider to notify and, for "trip started", where it's headed. The pickup
// point is the fallback location for an SOS alert.
type TripInfo struct {
	ID               string
	RiderID          string
	DriverID         string
	PickupLatitude   float64
	PickupLongitude  float64
	DropoffLatitude  float64
	DropoffLongitude float64
}

// TripClient is the handler's view of trip-service.
type TripClient interface {
	GetTrip(ctx context.Context, tripID string) (TripInfo, error)
}

// DriverInfo is the handler's view of a driver — enough to render the
// "driver assigned" notification.
type DriverInfo struct {
	ID           string
	DisplayName  string
	VehicleMake  string
	VehicleModel string
	VehicleColor string
	PlateNumber  string
}

// DriverClient is the handler's view of driver-service.
type DriverClient interface {
	GetDriver(ctx context.Context, driverID string) (DriverInfo, error)
}
