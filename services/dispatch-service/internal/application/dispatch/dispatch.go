package dispatch

import "context"

// DefaultSearchRadiusMeters is used when the caller doesn't specify one.
const DefaultSearchRadiusMeters = 5000

// candidateLimit is how many nearby drivers we consider before giving up.
// Kept generous because some candidates will be filtered out (already
// busy, or lost a race to accept another trip first).
const candidateLimit = 10

type TripInfo struct {
	ID        string
	RiderID   string
	Status    string
	PickupLat float64
	PickupLng float64
}

// TripClient is Dispatch's view of trip-service — just enough to read a
// trip's pickup point and status, and to accept it on a driver's behalf.
type TripClient interface {
	GetTrip(ctx context.Context, tripID string) (TripInfo, error)
	AcceptTrip(ctx context.Context, tripID string, driverID string) error
}

type NearbyDriver struct {
	DriverID       string
	DistanceMeters float64
}

// LocationClient is Dispatch's view of location-service.
type LocationClient interface {
	FindNearbyDrivers(
		ctx context.Context,
		latitude, longitude, radiusMeters float64,
		limit int,
	) ([]NearbyDriver, error)
}

type DriverInfo struct {
	ID                 string
	Status             string
	AvailabilityStatus string
}

// DriverClient is Dispatch's view of driver-service.
type DriverClient interface {
	GetDriver(ctx context.Context, driverID string) (DriverInfo, error)
	MarkBusy(ctx context.Context, driverID string) error
}

// Result is what a successful dispatch produces.
type Result struct {
	TripID         string
	DriverID       string
	DistanceMeters float64
}
