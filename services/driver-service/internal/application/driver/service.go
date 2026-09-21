package driver

import "context"

type CreateDriverInput struct {
	IdentityID   string
	DisplayName  string
	VehicleMake  string
	VehicleModel string
	VehicleColor string
	VehiclePlate string
	VehicleClass string
}

type UpdateDriverProfileInput struct {
	DriverID     string
	DisplayName  string
	VehicleMake  string
	VehicleModel string
	VehicleColor string
	VehiclePlate string
	VehicleClass string
}

type UpdateDriverAvailabilityInput struct {
	DriverID           string
	AvailabilityStatus AvailabilityStatus
}

// Service is the application-layer use-case boundary for the driver domain.
type Service interface {
	CreateDriver(
		ctx context.Context,
		input CreateDriverInput,
	) (Driver, error)

	GetDriver(
		ctx context.Context,
		driverID string,
	) (Driver, error)

	GetDriverByIdentityID(
		ctx context.Context,
		identityID string,
	) (Driver, error)

	UpdateDriverProfile(
		ctx context.Context,
		input UpdateDriverProfileInput,
	) (Driver, error)

	UpdateAvailability(
		ctx context.Context,
		input UpdateDriverAvailabilityInput,
	) (Driver, error)

	// ApproveDriver moves a pending (or rejected) driver to active.
	ApproveDriver(
		ctx context.Context,
		driverID string,
	) (Driver, error)

	// RejectDriver moves a pending driver to rejected.
	RejectDriver(
		ctx context.Context,
		driverID string,
	) (Driver, error)
}
