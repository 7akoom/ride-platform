package driver

import (
	"strings"
	"time"
)

// Status is the operator-controlled state of a driver account.
//
// A new driver is pending. Only an operator moves it to active (approved) or
// rejected. Suspended is a separate, later state for drivers who already worked.
type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusRejected  Status = "rejected"
	StatusSuspended Status = "suspended"
)

// CanGoOnline reports whether a driver in this status may be anything other
// than offline. Pending and rejected drivers never may. Suspended is left
// alone here on purpose: the trip-lifecycle reconciler must still be able to
// move a suspended driver from busy back to available after a running trip.
func (s Status) CanGoOnline() bool {
	return s != StatusPending && s != StatusRejected
}

type AvailabilityStatus string

const (
	AvailabilityOffline   AvailabilityStatus = "offline"
	AvailabilityAvailable AvailabilityStatus = "available"
	AvailabilityBusy      AvailabilityStatus = "busy"
)

func (a AvailabilityStatus) Valid() bool {
	switch a {
	case AvailabilityOffline, AvailabilityAvailable, AvailabilityBusy:
		return true
	default:
		return false
	}
}

// VehicleClass is the service tier a driver's vehicle qualifies for.
// Adding a class means adding a constant here, extending Valid, and
// widening the drivers_vehicle_class_check constraint in a migration.
type VehicleClass string

const (
	VehicleClassEconomy VehicleClass = "economy"
	VehicleClassComfort VehicleClass = "comfort"
)

func (c VehicleClass) Valid() bool {
	switch c {
	case VehicleClassEconomy, VehicleClassComfort:
		return true
	default:
		return false
	}
}

// DisplayName is a validated, trimmed driver display name.
type DisplayName struct {
	value string
}

func NewDisplayName(raw string) (DisplayName, error) {
	trimmed := strings.TrimSpace(raw)

	if trimmed == "" {
		return DisplayName{}, ErrDisplayNameRequired
	}

	if len([]rune(trimmed)) > 120 {
		return DisplayName{}, ErrDisplayNameTooLong
	}

	return DisplayName{value: trimmed}, nil
}

func (d DisplayName) String() string {
	return d.value
}

// Vehicle is a validated vehicle value object.
//
// Class may be empty when a caller did not specify one: CreateDriver turns
// that into economy, while UpdateDriverProfile leaves the stored class
// untouched. Once a Vehicle is read back from the database Class is
// always set.
type Vehicle struct {
	Make        string
	Model       string
	Color       string
	PlateNumber string
	Class       VehicleClass
}

func NewVehicle(make_, model, color, plateNumber, vehicleClass string) (Vehicle, error) {
	trimmedPlate := strings.TrimSpace(plateNumber)

	if strings.TrimSpace(make_) == "" ||
		strings.TrimSpace(model) == "" ||
		trimmedPlate == "" {
		return Vehicle{}, ErrVehicleFieldsRequired
	}

	class := VehicleClass(strings.ToLower(strings.TrimSpace(vehicleClass)))
	if class != "" && !class.Valid() {
		return Vehicle{}, ErrInvalidVehicleClass
	}

	return Vehicle{
		Make:        strings.TrimSpace(make_),
		Model:       strings.TrimSpace(model),
		Color:       strings.TrimSpace(color),
		PlateNumber: strings.ToUpper(trimmedPlate),
		Class:       class,
	}, nil
}

// Driver is the aggregate root for the driver domain.
type Driver struct {
	ID                 string
	IdentityID         string
	DisplayName        string
	Status             Status
	AvailabilityStatus AvailabilityStatus
	Vehicle            Vehicle
	RatingAverage      float64
	RatingCount        int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
