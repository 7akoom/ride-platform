package media

import (
	"errors"
	"time"
)

type Purpose string

const (
	PurposeDriverDocument    Purpose = "driver_document"
	PurposeProfilePhoto      Purpose = "profile_photo"
	PurposeAddressPhoto      Purpose = "address_photo"
	PurposeSupportAttachment Purpose = "support_attachment"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusReady    Status = "ready"
	StatusRejected Status = "rejected"
	StatusDeleted  Status = "deleted"
	StatusExpired  Status = "expired"
)

const (
	TypeJPEG = "image/jpeg"
	TypePNG  = "image/png"
	TypeWebP = "image/webp"
	TypePDF  = "application/pdf"
)

const megabyte = 1 << 20

// Policy is what a purpose accepts.
type Policy struct {
	Types    []string
	MaxBytes int64
}

var policies = map[Purpose]Policy{
	PurposeDriverDocument:    {Types: []string{TypeJPEG, TypePNG, TypeWebP, TypePDF}, MaxBytes: 10 * megabyte},
	PurposeProfilePhoto:      {Types: []string{TypeJPEG, TypePNG, TypeWebP}, MaxBytes: 5 * megabyte},
	PurposeAddressPhoto:      {Types: []string{TypeJPEG, TypePNG, TypeWebP}, MaxBytes: 8 * megabyte},
	PurposeSupportAttachment: {Types: []string{TypeJPEG, TypePNG, TypeWebP, TypePDF}, MaxBytes: 10 * megabyte},
}

// PolicyFor returns the policy of a purpose.
func PolicyFor(purpose Purpose) (Policy, bool) {
	policy, ok := policies[purpose]

	return policy, ok
}

func (p Policy) allows(contentType string) bool {
	for _, allowed := range p.Types {
		if allowed == contentType {
			return true
		}
	}

	return false
}

// Media is one uploaded (or reserved) file.
type Media struct {
	ID                  string
	OwnerIdentityID     string
	Purpose             Purpose
	Status              Status
	DeclaredContentType string
	DeclaredSize        int64
	ContentType         string
	SizeBytes           int64
	SHA256              string
	Width               int
	Height              int
	// ObjectKey holds the accepted bytes; the client uploads to UploadKey().
	ObjectKey       string
	UploadCleared   bool
	RejectionReason string
	Held            bool
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

// UploadKey is where the client sends the bytes. They are copied to
// ObjectKey only after the checks pass.
func (m Media) UploadKey() string {
	return "incoming/" + m.ObjectKey
}

// UploadTicket is where and how to send the bytes.
type UploadTicket struct {
	URL       string
	Method    string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ReadyInput is what the checks established about an accepted file.
type ReadyInput struct {
	ContentType string
	SizeBytes   int64
	SHA256      string
	Width       int
	Height      int
	At          time.Time
}

var (
	ErrInvalidPurpose = errors.New("purpose is not valid")
	ErrTypeNotAllowed = errors.New("this purpose does not accept this content type")
	ErrInvalidSize    = errors.New("size must be more than zero and within the purpose's limit")
	ErrInvalidID      = errors.New("media id is not valid")
	ErrOwnerRequired  = errors.New("a user's own access token is required")
	ErrTooManyPending = errors.New("too many uploads are waiting to be completed")
	ErrNotFound       = errors.New("media not found")
	ErrNotUploaded    = errors.New("the file has not been uploaded yet")
	ErrInvalidState   = errors.New("the file is not in a state that allows this")
	ErrNotReady       = errors.New("the file is not ready")
	ErrHeld           = errors.New("the file is in use and cannot be deleted")
	ErrHoldMismatch   = errors.New("the file does not belong to that owner or is not of that purpose")
	ErrObjectTooLarge = errors.New("the stored object is larger than allowed")
)
