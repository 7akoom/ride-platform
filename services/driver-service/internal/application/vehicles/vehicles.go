package vehicles

import (
	"context"
	"errors"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
	StatusRetired  Status = "retired"
)

// Vehicle is one of a driver's cars.
type Vehicle struct {
	ID              string
	DriverID        string
	Make            string
	Model           string
	Color           string
	PlateNumber     string
	Year            int
	Class           string
	Status          Status
	Active          bool
	RejectionReason string
	ReviewedBy      string
	ReviewedAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ApproveRecord approves a pending car with its final year and class.
type ApproveRecord struct {
	VehicleID  string
	Year       int
	Class      string
	ReviewedBy string
}

type RejectRecord struct {
	VehicleID  string
	Reason     string
	ReviewedBy string
}

type PendingQuery struct {
	AfterCreatedAt time.Time
	AfterID        string
	Limit          int
}

type PendingItem struct {
	Vehicle           Vehicle
	DriverDisplayName string
	DriverStatus      string
}

// Repository keeps drivers' cars. The active car's details are copied to the
// driver row in the same transaction that changes them.
type Repository interface {
	// Add returns ErrDriverNotFound, ErrTooManyVehicles, ErrPlateTaken.
	Add(ctx context.Context, v Vehicle, maxInService int) (Vehicle, error)
	// Get returns ErrVehicleNotFound.
	Get(ctx context.Context, id string) (Vehicle, error)
	ListByDriver(ctx context.Context, driverID string) ([]Vehicle, error)
	// Update changes a pending or rejected car (a rejected one goes back to
	// pending). ErrVehicleNotEditable, ErrPlateTaken.
	Update(ctx context.Context, v Vehicle) (Vehicle, error)
	// Activate makes an approved car of an offline driver the active one.
	// ErrVehicleNotApproved, ErrDriverNotOffline.
	Activate(ctx context.Context, driverID, vehicleID string) (Vehicle, error)
	// Retire takes a car that is not active out of service and supersedes its
	// documents, whose files it returns. ErrVehicleActive, ErrVehicleRetired.
	Retire(ctx context.Context, driverID, vehicleID string) (Vehicle, []string, error)
	// Approve and Reject only work on a pending car: ErrVehicleNotPending.
	Approve(ctx context.Context, record ApproveRecord) (Vehicle, error)
	Reject(ctx context.Context, record RejectRecord) (Vehicle, error)
	ListPending(ctx context.Context, query PendingQuery) ([]PendingItem, error)
}

// Documents checks a car's own documents.
type Documents interface {
	// VehicleDocumentsMissing names the car's required document types that are
	// not approved and in date.
	VehicleDocumentsMissing(ctx context.Context, driverID, vehicleID string) ([]string, error)
}

// Media deletes the files of a retired car's documents.
type Media interface {
	Discard(ctx context.Context, mediaID string) error
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}

var (
	ErrDriverNotFound      = errors.New("driver not found")
	ErrVehicleNotFound     = errors.New("vehicle not found")
	ErrInvalidVehicle      = errors.New("make, model and plate number are required (make and model up to 60 characters, color 40, plate 20)")
	ErrInvalidYear         = errors.New("year must be between 1980 and next year")
	ErrYearRequired        = errors.New("the year of the car is required")
	ErrInvalidClass        = errors.New("vehicle class must be economy or comfort")
	ErrTooManyVehicles     = errors.New("a driver can have at most 5 cars in service; retire one first")
	ErrPlateTaken          = errors.New("this plate number is already registered")
	ErrVehicleNotEditable  = errors.New("only a car that is pending or was rejected can be changed")
	ErrVehicleNotApproved  = errors.New("only an approved car can be made active")
	ErrDriverNotOffline    = errors.New("go offline before changing cars")
	ErrVehicleActive       = errors.New("the active car cannot be retired; make another one active first")
	ErrVehicleRetired      = errors.New("the car is retired")
	ErrVehicleNotPending   = errors.New("only a pending car can be reviewed")
	ErrDocumentsIncomplete = errors.New("the car's required documents are missing, not approved or out of date")
	ErrReasonRequired      = errors.New("a reason is required")
	ErrReasonTooLong       = errors.New("reason exceeds 500 characters")
	ErrInvalidPageToken    = errors.New("page_token is not valid")
	ErrInvalidPageSize     = errors.New("page_size is not valid")
)
