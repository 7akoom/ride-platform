package trip

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNoPickupPhoto: the trip's pickup has no photo.
	ErrNoPickupPhoto = errors.New("this trip's pickup has no photo")

	// ErrPickupPhotoNotAvailableNow: the photo is only shown while the trip is
	// accepted or in progress.
	ErrPickupPhotoNotAvailableNow = errors.New("the pickup photo is only available while the trip is accepted or in progress")
)

// PhotoLinks gives a short-lived link to a media-service file.
type PhotoLinks interface {
	DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error)
}

// WithPickupPhotos decorates a Service with PickupPhoto: a link to the photo of
// the saved pickup address, for the rider and the driver of the trip. Who may
// ask is decided before this is called; this decides when.
func WithPickupPhotos(base Service, links PhotoLinks) Service {
	if base == nil {
		panic("trip service is required")
	}

	if links == nil {
		panic("photo links are required")
	}

	return &pickupPhotoService{Service: base, links: links}
}

type pickupPhotoService struct {
	Service

	links PhotoLinks
}

func (s *pickupPhotoService) Unwrap() Service { return s.Service }

// PickupPhoto returns a link to the trip's pickup photo while the trip is
// accepted or in progress: the captain needs it to find the rider, nobody
// needs it after.
func (s *pickupPhotoService) PickupPhoto(ctx context.Context, tripID string) (string, time.Time, error) {
	found, err := s.GetTrip(ctx, tripID)
	if err != nil {
		return "", time.Time{}, err
	}

	if found.PickupPhotoMediaID == "" {
		return "", time.Time{}, ErrNoPickupPhoto
	}

	if found.Status != StatusAccepted && found.Status != StatusInProgress {
		return "", time.Time{}, ErrPickupPhotoNotAvailableNow
	}

	url, expires, err := s.links.DownloadURL(ctx, found.PickupPhotoMediaID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("pickup photo link: %w", err)
	}

	return url, expires, nil
}
