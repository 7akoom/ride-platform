package driver

import (
	"strings"
	"time"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

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
type Vehicle struct {
	Make        string
	Model       string
	Color       string
	PlateNumber string
}

func NewVehicle(make_, model, color, plateNumber string) (Vehicle, error) {
	trimmedPlate := strings.TrimSpace(plateNumber)

	if strings.TrimSpace(make_) == "" ||
		strings.TrimSpace(model) == "" ||
		trimmedPlate == "" {
		return Vehicle{}, ErrVehicleFieldsRequired
	}

	return Vehicle{
		Make:        strings.TrimSpace(make_),
		Model:       strings.TrimSpace(model),
		Color:       strings.TrimSpace(color),
		PlateNumber: strings.ToUpper(trimmedPlate),
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
