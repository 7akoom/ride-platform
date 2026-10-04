package clients

import (
	"context"
	"fmt"
	"time"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/address"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/profile"
)

const mediaCallTimeout = 5 * time.Second

// MediaPhotos keeps saved-address photos in media-service, as a service
// (internal token).
type MediaPhotos struct {
	media mediav1.MediaServiceClient
}

var (
	_ address.Photos = (*MediaPhotos)(nil)
	_ profile.Media  = (*MediaPhotos)(nil)
)

func NewMediaPhotos(conn grpc.ClientConnInterface) *MediaPhotos {
	if conn == nil {
		panic("media-service connection is required")
	}

	return &MediaPhotos{media: mediav1.NewMediaServiceClient(conn)}
}

// Hold asks media-service to keep the photo for the address. It refuses a
// file that is not a READY address photo of that identity.
func (m *MediaPhotos) Hold(ctx context.Context, mediaID, ownerIdentityID string) error {
	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	_, err := m.media.HoldMedia(ctx, &mediav1.HoldMediaRequest{
		MediaId:         mediaID,
		OwnerIdentityId: ownerIdentityID,
		Purpose:         mediav1.MediaPurpose_MEDIA_PURPOSE_ADDRESS_PHOTO,
	})

	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument:
		return address.ErrPhotoNotUsable
	default:
		return fmt.Errorf("%w: %v", address.ErrPhotoUnavailable, err)
	}
}

func (m *MediaPhotos) Release(ctx context.Context, mediaID string) error {
	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	if _, err := m.media.ReleaseMedia(ctx, &mediav1.ReleaseMediaRequest{MediaId: mediaID}); err != nil &&
		status.Code(err) != codes.NotFound {
		return fmt.Errorf("release photo: %w", err)
	}

	return nil
}

// Discard releases the photo and deletes it.
func (m *MediaPhotos) Discard(ctx context.Context, mediaID string) error {
	if err := m.Release(ctx, mediaID); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	if _, err := m.media.DeleteMedia(ctx, &mediav1.DeleteMediaRequest{MediaId: mediaID}); err != nil &&
		status.Code(err) != codes.NotFound {
		return fmt.Errorf("delete photo: %w", err)
	}

	return nil
}

// HoldProfilePhoto keeps the rider's picture. It refuses a file that is not a
// READY profile photo of that identity.
func (m *MediaPhotos) HoldProfilePhoto(ctx context.Context, mediaID, ownerIdentityID string) error {
	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	_, err := m.media.HoldMedia(ctx, &mediav1.HoldMediaRequest{
		MediaId:         mediaID,
		OwnerIdentityId: ownerIdentityID,
		Purpose:         mediav1.MediaPurpose_MEDIA_PURPOSE_PROFILE_PHOTO,
	})

	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument, codes.PermissionDenied:
		return profile.ErrPhotoNotUsable
	default:
		return fmt.Errorf("%w: %v", profile.ErrMediaUnavailable, err)
	}
}

// DownloadURL is a short-lived link to a file this service holds.
func (m *MediaPhotos) DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	response, err := m.media.GetDownloadURL(ctx, &mediav1.GetDownloadURLRequest{MediaId: mediaID})
	if status.Code(err) == codes.NotFound || status.Code(err) == codes.FailedPrecondition {
		return "", time.Time{}, profile.ErrNoPhoto
	}

	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: %v", profile.ErrMediaUnavailable, err)
	}

	return response.GetUrl(), response.GetExpiresAt().AsTime(), nil
}
