package documents

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MediaPurpose is the media-service purpose a document's file is uploaded with.
type MediaPurpose string

const (
	PurposeDriverDocument MediaPurpose = "driver_document"
	PurposeProfilePhoto   MediaPurpose = "profile_photo"
)

func (p MediaPurpose) Valid() bool {
	return p == PurposeDriverDocument || p == PurposeProfilePhoto
}

// Scope says whose document a type is: the driver's own, or one car's.
type Scope string

const (
	ScopeDriver  Scope = "driver"
	ScopeVehicle Scope = "vehicle"
)

func (s Scope) Valid() bool {
	return s == ScopeDriver || s == ScopeVehicle
}

// Type is one kind of document a driver is asked for.
type Type struct {
	Code           string
	Scope          Scope
	MediaPurpose   MediaPurpose
	NameEN         string
	NameAR         string
	NameKU         string
	Required       bool
	RequiresNumber bool
	RequiresExpiry bool
	Active         bool
	SortOrder      int
}

type Status string

const (
	StatusPending    Status = "pending"
	StatusApproved   Status = "approved"
	StatusRejected   Status = "rejected"
	StatusSuperseded Status = "superseded"
)

// Document is one file a driver handed in for a type.
type Document struct {
	ID       string
	DriverID string
	// VehicleID is the car a vehicle document is for; empty otherwise.
	VehicleID       string
	TypeCode        string
	MediaID         string
	Number          string
	ExpiresOn       Date
	Status          Status
	RejectionReason string
	ReviewedBy      string
	ReviewedAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ExpiredOn reports whether an approved document is out of date on day today.
func (d Document) ExpiredOn(today Date) bool {
	return d.Status == StatusApproved && !d.ExpiresOn.IsZero() && d.ExpiresOn.Before(today)
}

// Date is a calendar day. The zero Date means "no date".
type Date struct {
	t time.Time
}

const dateLayout = "2006-01-02"

// ParseDate reads YYYY-MM-DD; an empty string is the zero Date.
func ParseDate(raw string) (Date, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Date{}, nil
	}

	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		return Date{}, ErrInvalidExpiryDate
	}

	return Date{t: t}, nil
}

// DateOf is the calendar day of t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()

	return Date{t: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// NewDate builds a Date from its parts.
func NewDate(year int, month time.Month, day int) Date {
	return Date{t: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

func (d Date) IsZero() bool             { return d.t.IsZero() }
func (d Date) Before(other Date) bool   { return d.t.Before(other.t) }
func (d Date) After(other Date) bool    { return d.t.After(other.t) }
func (d Date) AddDays(days int) Date    { return Date{t: d.t.AddDate(0, 0, days)} }
func (d Date) Time() time.Time          { return d.t }
func (d Date) DaysUntil(other Date) int { return int(other.t.Sub(d.t).Hours() / 24) }

func (d Date) String() string {
	if d.t.IsZero() {
		return ""
	}

	return d.t.Format(dateLayout)
}

// DateFromTime keeps the calendar day of a DATE column read as UTC midnight.
func DateFromTime(t time.Time) Date {
	if t.IsZero() {
		return Date{}
	}

	y, m, d := t.Date()

	return NewDate(y, m, d)
}

const (
	maxNumberLength   = 50
	minNumberLength   = 3
	maxReasonLength   = 500
	maxNameLength     = 80
	maxSortOrder      = 1000
	maxExpiryAheadYrs = 20
)

// NormalizeNumber trims a document number, folds runs of spaces and upper-cases
// Latin letters. Letters of any script, digits, spaces, '-' and '/' are allowed.
func NormalizeNumber(raw string) (string, error) {
	folded := strings.ToUpper(strings.Join(strings.Fields(raw), " "))

	if folded == "" {
		return "", ErrNumberRequired
	}

	n := utf8.RuneCountInString(folded)
	if n < minNumberLength || n > maxNumberLength {
		return "", ErrInvalidNumber
	}

	for _, r := range folded {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '-' && r != '/' {
			return "", ErrInvalidNumber
		}
	}

	return folded, nil
}

var (
	ErrDriverNotFound        = errors.New("driver not found")
	ErrTypeNotFound          = errors.New("document type not found")
	ErrDocumentNotFound      = errors.New("document not found")
	ErrInvalidTypeCode       = errors.New("document type code must be 2-40 lower-case letters, digits or '_', starting with a letter")
	ErrInvalidType           = errors.New("document type is not valid: names 1-80 characters, purpose driver_document or profile_photo, scope driver or vehicle, sort order 0-1000")
	ErrMediaIDRequired       = errors.New("media_id is required")
	ErrInvalidMediaID        = errors.New("media_id is not valid")
	ErrMediaNotUsable        = errors.New("the file is not a ready upload of this driver, of the purpose this document type needs")
	ErrMediaAlreadyUsed      = errors.New("the file is already used by another document")
	ErrMediaUnavailable      = errors.New("media-service is unavailable")
	ErrNumberRequired        = errors.New("document_number is required for this document type")
	ErrInvalidNumber         = errors.New("document_number must be 3-50 letters, digits, spaces, '-' or '/'")
	ErrExpiryRequired        = errors.New("expires_on is required for this document type")
	ErrInvalidExpiryDate     = errors.New("expires_on must be a date, YYYY-MM-DD")
	ErrExpiryNotInFuture     = errors.New("expires_on must be after today")
	ErrExpiryTooFar          = errors.New("expires_on is too far ahead")
	ErrNumberTaken           = errors.New("this document number is already approved for another driver")
	ErrDocumentNotPending    = errors.New("only a pending document can be approved")
	ErrDocumentNotReviewable = errors.New("only a pending or approved document can be rejected")
	ErrReasonRequired        = errors.New("a reason is required")
	ErrReasonTooLong         = errors.New("reason exceeds 500 characters")
	ErrInvalidPageToken      = errors.New("page_token is not valid")
	ErrInvalidPageSize       = errors.New("page_size is not valid")
	ErrVehicleNotFound       = errors.New("vehicle not found")
	ErrNoActiveVehicle       = errors.New("the driver has no active car to hand this document in for")
	ErrVehicleRetired        = errors.New("the car is retired")
)
