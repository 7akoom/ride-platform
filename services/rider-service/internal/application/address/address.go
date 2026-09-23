// Package address holds a rider's saved addresses: home, work and other
// places, with what helps the captain find them (building and floor, a note,
// a photo of the entrance).
package address

import (
	"context"
	"errors"
	"time"
)

type Kind string

const (
	KindHome  Kind = "home"
	KindWork  Kind = "work"
	KindOther Kind = "other"
)

func (k Kind) Valid() bool {
	return k == KindHome || k == KindWork || k == KindOther
}

const (
	MaxPerRider      = 20
	MaxLabelLength   = 60
	MaxAddressLength = 300
	MaxDetailsLength = 200
	MaxNoteLength    = 300
)

var (
	ErrAddressIDRequired = errors.New("address id is required")
	ErrAddressNotFound   = errors.New("saved address not found")
	ErrRiderNotFound     = errors.New("rider not found")
	ErrInvalidKind       = errors.New("kind must be home, work or other")
	ErrLabelRequired     = errors.New("a label is required for other addresses")
	ErrLabelTooLong      = errors.New("label is longer than 60 characters")
	ErrAddressTooLong    = errors.New("address is longer than 300 characters")
	ErrDetailsTooLong    = errors.New("details are longer than 200 characters")
	ErrNoteTooLong       = errors.New("note for the driver is longer than 300 characters")
	ErrPointRequired     = errors.New("coordinates are required")
	ErrInvalidLatitude   = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude  = errors.New("longitude must be between -180 and 180")
	ErrKindTaken         = errors.New("the rider already has a saved address of this kind")
	ErrTooManyAddresses  = errors.New("the rider has 20 saved addresses already")
	ErrInvalidPhotoID    = errors.New("photo media id is not valid")
	ErrPhotoConflict     = errors.New("set photo_media_id or remove_photo, not both")
	// ErrPhotoNotUsable: the photo is not a READY address photo of this rider.
	ErrPhotoNotUsable = errors.New("the photo is not a ready address photo uploaded by this rider")
	// ErrPhotoUnavailable: media-service could not be asked.
	ErrPhotoUnavailable = errors.New("the photo could not be checked right now")
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Address struct {
	ID            string
	RiderID       string
	Kind          Kind
	Label         string
	Coordinates   Coordinates
	Address       string
	Details       string
	NoteForDriver string
	PhotoMediaID  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Details is what the rider sets on an address, photo aside.
type Details struct {
	Kind          Kind
	Label         string
	Coordinates   Coordinates
	Address       string
	Details       string
	NoteForDriver string
}

// Repository persists saved addresses. Create returns ErrTooManyAddresses
// when the rider has MaxPerRider already (checked under a lock on the rider,
// so two creates cannot both pass); Create and Update return ErrKindTaken when
// the rider has another home (or work) address; Get, Update and Delete return
// ErrAddressNotFound for an address that is not the rider's.
type Repository interface {
	Create(ctx context.Context, a Address) (Address, error)
	Update(ctx context.Context, a Address) (Address, error)
	Get(ctx context.Context, riderID, id string) (Address, error)
	List(ctx context.Context, riderID string) ([]Address, error)
	Delete(ctx context.Context, riderID, id string) (Address, error)
}

// Riders tells whose identity a rider profile is: photos belong to identities.
type Riders interface {
	IdentityOf(ctx context.Context, riderID string) (string, error)
}

// Photos keeps address photos in media-service. Hold makes media-service
// keep the file for this address (and checks that it is a READY address
// photo of that identity: ErrPhotoNotUsable otherwise). Release undoes Hold;
// Discard releases and deletes it.
type Photos interface {
	Hold(ctx context.Context, mediaID, ownerIdentityID string) error
	Release(ctx context.Context, mediaID string) error
	Discard(ctx context.Context, mediaID string) error
}

type IDGenerator interface {
	NewID() string
}
