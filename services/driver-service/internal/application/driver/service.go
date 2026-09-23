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

	// RejectDriver moves a pending driver to rejected, keeping the reason
	// the driver is shown.
	RejectDriver(
		ctx context.Context,
		driverID string,
		reason string,
	) (Driver, error)

	// ListDrivers returns one page of drivers, newest first.
	ListDrivers(
		ctx context.Context,
		query ListDriversQuery,
	) (DriversPage, error)
}

// ListDriversQuery asks for one page of drivers, optionally of one status.
type ListDriversQuery struct {
	Status    Status
	PageSize  int
	PageToken string
}

// DriversPage is one page of drivers. NextPageToken is empty on the last page.
type DriversPage struct {
	Drivers       []Driver
	NextPageToken string
}
