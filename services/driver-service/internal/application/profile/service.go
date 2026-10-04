package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

var (
	// ErrDetailsLocked: an approved driver's given detail changes only through support.
	ErrDetailsLocked = errors.New("details already given change only through support once a driver is approved")
	// ErrNoPhoto: the driver has no approved profile photo.
	ErrNoPhoto = errors.New("no approved profile photo")
	// ErrMediaUnavailable: media-service did not answer.
	ErrMediaUnavailable = errors.New("media-service is unavailable")

	ErrNameChangeNotFound   = errors.New("name change not found")
	ErrNameChangeOpen       = errors.New("a name change is already waiting for review")
	ErrNameChangeNotPending = errors.New("the name change is not waiting for review")
	ErrNameUnchanged        = errors.New("the requested name is the current one")
	ErrNameChangeNotNeeded  = errors.New("a driver not yet approved changes the name in the profile")
	ErrInvalidName          = errors.New("a name is 1-120 characters")
	ErrInvalidReason        = errors.New("a reason is at most 300 characters")
	ErrRejectionRequired    = errors.New("a rejection reason is required, at most 500 characters")
)

type Details struct {
	DriverID  string
	Fields    Fields
	HasPhoto  bool
	UpdatedAt time.Time
}

type NameChangeStatus string

const (
	NameChangePending  NameChangeStatus = "pending"
	NameChangeApproved NameChangeStatus = "approved"
	NameChangeRejected NameChangeStatus = "rejected"
)

type NameChange struct {
	ID              string
	DriverID        string
	CurrentName     string
	RequestedName   string
	Reason          string
	Status          NameChangeStatus
	RejectionReason string
	DecidedBy       string
	CreatedAt       time.Time
	DecidedAt       *time.Time
}

type Repository interface {
	GetDetails(ctx context.Context, driverID string) (Details, error)
	SaveFields(ctx context.Context, driverID string, fields Fields, at time.Time) (Details, error)
	// ApprovedPhoto is the media id of the driver's approved profile photo.
	ApprovedPhoto(ctx context.Context, driverID string) (string, bool, error)

	// CreateNameChange returns ErrNameChangeOpen while one is pending.
	CreateNameChange(ctx context.Context, change NameChange) (NameChange, error)
	ListNameChanges(ctx context.Context, driverID string) ([]NameChange, error)
	ListPendingNameChanges(ctx context.Context, limit int) ([]NameChange, error)
	// DecideNameChange locks the request; approving also renames the driver
	// and both write a driver.name_change_reviewed event.
	DecideNameChange(ctx context.Context, id string, decide func(change *NameChange) error) (NameChange, error)
}

type Drivers interface {
	GetDriver(ctx context.Context, driverID string) (driver.Driver, error)
}

type Media interface {
	DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error)
}

type IDGenerator interface{ NewID() string }

type Clock interface{ Now() time.Time }

type Service struct {
	repository Repository
	drivers    Drivers
	media      Media
	ids        IDGenerator
	clock      Clock
}

func NewService(repository Repository, drivers Drivers, media Media, ids IDGenerator, clock Clock) *Service {
	if repository == nil || drivers == nil || media == nil || ids == nil || clock == nil {
		panic("profile service dependencies are required")
	}

	return &Service{repository: repository, drivers: drivers, media: media, ids: ids, clock: clock}
}

func approved(d driver.Driver) bool {
	return d.Status == driver.StatusActive || d.Status == driver.StatusSuspended
}

func (s *Service) Get(ctx context.Context, driverID string) (Details, error) {
	found, err := s.drivers.GetDriver(ctx, strings.TrimSpace(driverID))
	if err != nil {
		return Details{}, err
	}

	return s.repository.GetDetails(ctx, found.ID)
}

// Update applies the patch. Once approved, a detail already given stays as
// it is; an empty one can be filled in.
func (s *Service) Update(ctx context.Context, driverID string, patch Patch) (Details, error) {
	found, err := s.drivers.GetDriver(ctx, strings.TrimSpace(driverID))
	if err != nil {
		return Details{}, err
	}

	current, err := s.repository.GetDetails(ctx, found.ID)
	if err != nil {
		return Details{}, err
	}

	now := s.clock.Now()

	next, err := patch.Apply(current.Fields, now)
	if err != nil {
		return Details{}, err
	}

	if next == current.Fields {
		return current, nil
	}

	if approved(found) {
		was, is := current.Fields, next
		if (was.Gender != "" && was.Gender != is.Gender) ||
			(was.DateOfBirth != "" && was.DateOfBirth != is.DateOfBirth) ||
			(was.Nationality != "" && was.Nationality != is.Nationality) {
			return Details{}, ErrDetailsLocked
		}
	}

	return s.repository.SaveFields(ctx, found.ID, next, now)
}

func (s *Service) PhotoURL(ctx context.Context, driverID string) (string, time.Time, error) {
	found, err := s.drivers.GetDriver(ctx, strings.TrimSpace(driverID))
	if err != nil {
		return "", time.Time{}, err
	}

	mediaID, ok, err := s.repository.ApprovedPhoto(ctx, found.ID)
	if err != nil {
		return "", time.Time{}, err
	}

	if !ok {
		return "", time.Time{}, ErrNoPhoto
	}

	url, expires, err := s.media.DownloadURL(ctx, mediaID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("photo link: %w", err)
	}

	return url, expires, nil
}

func (s *Service) RequestNameChange(ctx context.Context, driverID, requestedName, reason string) (NameChange, error) {
	found, err := s.drivers.GetDriver(ctx, strings.TrimSpace(driverID))
	if err != nil {
		return NameChange{}, err
	}

	name := strings.Join(strings.Fields(requestedName), " ")
	reason = strings.TrimSpace(reason)

	switch {
	case name == "" || utf8.RuneCountInString(name) > 120:
		return NameChange{}, ErrInvalidName
	case utf8.RuneCountInString(reason) > 300:
		return NameChange{}, ErrInvalidReason
	case !approved(found):
		return NameChange{}, ErrNameChangeNotNeeded
	case name == found.DisplayName:
		return NameChange{}, ErrNameUnchanged
	}

	return s.repository.CreateNameChange(ctx, NameChange{
		ID:            s.ids.NewID(),
		DriverID:      found.ID,
		CurrentName:   found.DisplayName,
		RequestedName: name,
		Reason:        reason,
		Status:        NameChangePending,
		CreatedAt:     s.clock.Now(),
	})
}

func (s *Service) ListNameChanges(ctx context.Context, driverID string) ([]NameChange, error) {
	found, err := s.drivers.GetDriver(ctx, strings.TrimSpace(driverID))
	if err != nil {
		return nil, err
	}

	return s.repository.ListNameChanges(ctx, found.ID)
}

func (s *Service) ListPendingNameChanges(ctx context.Context, limit int) ([]NameChange, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	return s.repository.ListPendingNameChanges(ctx, limit)
}

func (s *Service) ApproveNameChange(ctx context.Context, id, reviewer string) (NameChange, error) {
	return s.repository.DecideNameChange(ctx, strings.TrimSpace(id), func(c *NameChange) error {
		if c.Status != NameChangePending {
			return ErrNameChangeNotPending
		}

		now := s.clock.Now()
		c.Status = NameChangeApproved
		c.DecidedBy = reviewer
		c.DecidedAt = &now

		return nil
	})
}

func (s *Service) RejectNameChange(ctx context.Context, id, reviewer, reason string) (NameChange, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 500 {
		return NameChange{}, ErrRejectionRequired
	}

	return s.repository.DecideNameChange(ctx, strings.TrimSpace(id), func(c *NameChange) error {
		if c.Status != NameChangePending {
			return ErrNameChangeNotPending
		}

		now := s.clock.Now()
		c.Status = NameChangeRejected
		c.RejectionReason = reason
		c.DecidedBy = reviewer
		c.DecidedAt = &now

		return nil
	})
}
