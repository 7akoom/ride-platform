package rider

import (
	"strings"
	"time"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusSuspended:
		return true
	default:
		return false
	}
}

// DisplayName is a validated, trimmed rider display name.
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

// Rider is the aggregate root for the rider domain.
type Rider struct {
	ID            string
	IdentityID    string
	DisplayName   string
	Status        Status
	RatingAverage float64
	RatingCount   int32
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
