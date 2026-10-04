package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
)

var (
	// ErrPhotoNotUsable: not a READY profile photo upload of the rider.
	ErrPhotoNotUsable = errors.New("the photo is not a ready profile photo of yours")
	// ErrNoPhoto: the rider has no photo.
	ErrNoPhoto = errors.New("no photo")
	// ErrMediaUnavailable: media-service did not answer.
	ErrMediaUnavailable = errors.New("media-service is unavailable")
)

// Details are a rider's personal details.
type Details struct {
	RiderID      string
	Fields       Fields
	PhotoMediaID string
	UpdatedAt    time.Time
}

type Repository interface {
	// GetDetails returns the rider's details; empty ones when none were given.
	GetDetails(ctx context.Context, riderID string) (Details, error)
	SaveFields(ctx context.Context, riderID string, fields Fields, at time.Time) (Details, error)
	// SetPhoto stores the photo (empty: none) and returns the one it replaced.
	SetPhoto(ctx context.Context, riderID, mediaID string, at time.Time) (Details, string, error)
}

type Riders interface {
	GetRider(ctx context.Context, riderID string) (rider.Rider, error)
}

type Media interface {
	HoldProfilePhoto(ctx context.Context, mediaID, ownerIdentityID string) error
	Discard(ctx context.Context, mediaID string) error
	DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	repository Repository
	riders     Riders
	media      Media
	clock      Clock
	logger     *slog.Logger
}

func NewService(repository Repository, riders Riders, media Media, clock Clock, logger *slog.Logger) *Service {
	if repository == nil || riders == nil || media == nil || clock == nil || logger == nil {
		panic("profile service dependencies are required")
	}

	return &Service{repository: repository, riders: riders, media: media, clock: clock, logger: logger}
}

func (s *Service) rider(ctx context.Context, riderID string) (rider.Rider, error) {
	found, err := s.riders.GetRider(ctx, strings.TrimSpace(riderID))
	if err != nil {
		return rider.Rider{}, err
	}

	return found, nil
}

func (s *Service) Get(ctx context.Context, riderID string) (Details, error) {
	found, err := s.rider(ctx, riderID)
	if err != nil {
		return Details{}, err
	}

	return s.repository.GetDetails(ctx, found.ID)
}

func (s *Service) Update(ctx context.Context, riderID string, patch Patch) (Details, error) {
	found, err := s.rider(ctx, riderID)
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

	return s.repository.SaveFields(ctx, found.ID, next, now)
}

// SetPhoto keeps the upload as the rider's photo and deletes the old one.
func (s *Service) SetPhoto(ctx context.Context, riderID, mediaID string) (Details, error) {
	found, err := s.rider(ctx, riderID)
	if err != nil {
		return Details{}, err
	}

	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return Details{}, ErrPhotoNotUsable
	}

	current, err := s.repository.GetDetails(ctx, found.ID)
	if err != nil {
		return Details{}, err
	}

	if current.PhotoMediaID == mediaID {
		return current, nil
	}

	if err := s.media.HoldProfilePhoto(ctx, mediaID, found.IdentityID); err != nil {
		return Details{}, err
	}

	saved, old, err := s.repository.SetPhoto(ctx, found.ID, mediaID, s.clock.Now())
	if err != nil {
		if discardErr := s.media.Discard(context.WithoutCancel(ctx), mediaID); discardErr != nil {
			s.logger.WarnContext(ctx, "failed to let go of an unused profile photo", "media_id", mediaID, "error", discardErr)
		}

		return Details{}, err
	}

	s.discard(ctx, old)

	return saved, nil
}

func (s *Service) DeletePhoto(ctx context.Context, riderID string) (Details, error) {
	found, err := s.rider(ctx, riderID)
	if err != nil {
		return Details{}, err
	}

	saved, old, err := s.repository.SetPhoto(ctx, found.ID, "", s.clock.Now())
	if err != nil {
		return Details{}, err
	}

	s.discard(ctx, old)

	return saved, nil
}

func (s *Service) discard(ctx context.Context, mediaID string) {
	if mediaID == "" {
		return
	}

	if err := s.media.Discard(context.WithoutCancel(ctx), mediaID); err != nil {
		s.logger.WarnContext(ctx, "failed to delete a replaced profile photo", "media_id", mediaID, "error", err)
	}
}

func (s *Service) PhotoURL(ctx context.Context, riderID string) (string, time.Time, error) {
	details, err := s.Get(ctx, riderID)
	if err != nil {
		return "", time.Time{}, err
	}

	if details.PhotoMediaID == "" {
		return "", time.Time{}, ErrNoPhoto
	}

	url, expires, err := s.media.DownloadURL(ctx, details.PhotoMediaID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("photo link: %w", err)
	}

	return url, expires, nil
}
