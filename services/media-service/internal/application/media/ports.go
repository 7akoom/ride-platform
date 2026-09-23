package media

import (
	"context"
	"time"
)

// Repository persists media records. Mark* only move a record out of the
// state named in their doc and return ErrInvalidState otherwise, so two
// concurrent calls can never both win.
type Repository interface {
	Create(ctx context.Context, media Media) error
	FindByID(ctx context.Context, id string) (Media, error)
	CountPending(ctx context.Context, ownerIdentityID string) (int, error)

	// MarkReady and MarkRejected move a pending record.
	MarkReady(ctx context.Context, id string, input ReadyInput) (Media, error)
	MarkRejected(ctx context.Context, id string, reason string, at time.Time) (Media, error)
	// MarkDeleted moves a pending, ready or rejected record that is not held.
	MarkDeleted(ctx context.Context, id string, at time.Time) (Media, error)
	// SetHeld holds or releases a ready record.
	SetHeld(ctx context.Context, id string, held bool) (Media, error)

	// ListStalePending returns pending records created before cutoff.
	ListStalePending(ctx context.Context, cutoff time.Time, limit int) ([]Media, error)
	// MarkExpired moves a pending record (and records its incoming object as
	// cleared: the caller deleted it and its upload URL has expired).
	MarkExpired(ctx context.Context, id string, at time.Time) error

	// ListUploadsToClear returns records past pending whose incoming object
	// was not yet cleared and that were created before cutoff.
	ListUploadsToClear(ctx context.Context, cutoff time.Time, limit int) ([]Media, error)
	// MarkUploadCleared records that a record's incoming object is gone.
	MarkUploadCleared(ctx context.Context, id string) error
}

// ObjectStore holds the bytes. Stat and Read return ErrNotUploaded for an
// object that is not there, and Read returns ErrObjectTooLarge past its limit.
type ObjectStore interface {
	PresignPut(key string, contentType string, size int64, ttl time.Duration) (string, map[string]string, time.Time)
	PresignGet(key string, ttl time.Duration) (string, time.Time)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Read(ctx context.Context, key string, limit int64) ([]byte, error)
	Put(ctx context.Context, key string, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// ObjectInfo is an object's size, as the store reports it.
type ObjectInfo struct {
	Size int64
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}
