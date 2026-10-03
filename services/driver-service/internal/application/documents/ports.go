package documents

import (
	"context"
	"time"
)

// ApproveRecord approves a pending document with its final number and expiry.
type ApproveRecord struct {
	DocumentID string
	Number     string
	ExpiresOn  Date
	ReviewedBy string
	Type       Type
}

// RejectRecord turns down a document that is still in ExpectedStatus.
// TakeOffline also puts an available driver offline (a withdrawn required document).
type RejectRecord struct {
	DocumentID     string
	ExpectedStatus Status
	Reason         string
	ReviewedBy     string
	TakeOffline    bool
	Type           Type
}

// PendingQuery selects one page of the review queue, oldest first, after a position.
type PendingQuery struct {
	AfterCreatedAt time.Time
	AfterID        string
	Limit          int
}

// PendingItem is a document waiting for review, with who handed it in.
type PendingItem struct {
	Document          Document
	DriverDisplayName string
	DriverStatus      string
}

// ExpiryRound is what one pass of the expiry check did.
type ExpiryRound struct {
	Reminded    int
	Expired     int
	TookOffline int
}

// Repository keeps document types and documents. Every change that tells the
// driver something writes its event to the outbox in the same transaction.
type Repository interface {
	ListTypes(ctx context.Context, includeInactive bool) ([]Type, error)
	// GetType returns ErrTypeNotFound for an unknown code.
	GetType(ctx context.Context, code string) (Type, error)
	UpsertType(ctx context.Context, t Type) (Type, error)

	// Submit inserts a pending document and supersedes the driver's pending or
	// rejected ones of the same type, which it returns. ErrMediaAlreadyUsed
	// when the file backs another document.
	Submit(ctx context.Context, d Document) (created Document, superseded []Document, err error)
	// Get returns ErrDocumentNotFound for an unknown id.
	Get(ctx context.Context, id string) (Document, error)
	// ListByDriver returns the driver's documents, newest first; superseded
	// ones only when asked.
	ListByDriver(ctx context.Context, driverID string, includeSuperseded bool) ([]Document, error)
	// Approve approves the document if it is still pending and supersedes the
	// driver's approved one of that type, which it returns.
	// ErrDocumentNotPending, ErrNumberTaken.
	Approve(ctx context.Context, record ApproveRecord) (approved Document, superseded []Document, err error)
	// Reject returns ErrDocumentNotReviewable when it is no longer in ExpectedStatus.
	Reject(ctx context.Context, record RejectRecord) (Document, error)
	ListPending(ctx context.Context, query PendingQuery) ([]PendingItem, error)

	// RunExpiry sends the reminders due on today (one per threshold, the
	// smallest reached), marks approved documents out of date on today, and
	// puts available drivers whose required document ran out offline.
	RunExpiry(ctx context.Context, today Date, thresholds []int, limit int) (ExpiryRound, error)
}

// VehicleRef is what documents need to know about a car.
type VehicleRef struct {
	ID       string
	DriverID string
	// Status is pending, approved, rejected or retired.
	Status string
	Active bool
}

// Vehicles finds a driver's cars.
type Vehicles interface {
	// ActiveVehicle reports the driver's active car; found is false without one.
	ActiveVehicle(ctx context.Context, driverID string) (ref VehicleRef, found bool, err error)
	// Vehicle returns ErrVehicleNotFound for an unknown car.
	Vehicle(ctx context.Context, vehicleID string) (VehicleRef, error)
}

// Drivers finds who a driver is, to check a file belongs to them.
type Drivers interface {
	// IdentityOf returns ErrDriverNotFound for an unknown driver.
	IdentityOf(ctx context.Context, driverID string) (string, error)
}

// Media keeps document files in media-service.
type Media interface {
	// Hold returns ErrMediaNotUsable for a file that is not a READY upload of
	// that identity and purpose, ErrMediaUnavailable when it cannot tell.
	Hold(ctx context.Context, mediaID, ownerIdentityID string, purpose MediaPurpose) error
	Release(ctx context.Context, mediaID string) error
	// Discard releases and deletes the file.
	Discard(ctx context.Context, mediaID string) error
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}
