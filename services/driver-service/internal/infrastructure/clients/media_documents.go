package clients

import (
	"context"
	"fmt"
	"time"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/profile"
)

const mediaCallTimeout = 5 * time.Second

// MediaDocuments keeps document files in media-service, as a service
// (internal token).
type MediaDocuments struct {
	media mediav1.MediaServiceClient
}

var (
	_ documents.Media = (*MediaDocuments)(nil)
	_ profile.Media   = (*MediaDocuments)(nil)
)

func NewMediaDocuments(conn grpc.ClientConnInterface) *MediaDocuments {
	if conn == nil {
		panic("media-service connection is required")
	}

	return &MediaDocuments{media: mediav1.NewMediaServiceClient(conn)}
}

// Hold asks media-service to keep the file for the document. It refuses a
// file that is not a READY upload of that identity and purpose.
func (m *MediaDocuments) Hold(ctx context.Context, mediaID, ownerIdentityID string, purpose documents.MediaPurpose) error {
	protoPurpose := mediav1.MediaPurpose_MEDIA_PURPOSE_DRIVER_DOCUMENT
	if purpose == documents.PurposeProfilePhoto {
		protoPurpose = mediav1.MediaPurpose_MEDIA_PURPOSE_PROFILE_PHOTO
	}

	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	_, err := m.media.HoldMedia(ctx, &mediav1.HoldMediaRequest{
		MediaId:         mediaID,
		OwnerIdentityId: ownerIdentityID,
		Purpose:         protoPurpose,
	})

	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument, codes.PermissionDenied:
		return documents.ErrMediaNotUsable
	default:
		return fmt.Errorf("%w: %v", documents.ErrMediaUnavailable, err)
	}
}

func (m *MediaDocuments) Release(ctx context.Context, mediaID string) error {
	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	if _, err := m.media.ReleaseMedia(ctx, &mediav1.ReleaseMediaRequest{MediaId: mediaID}); err != nil &&
		status.Code(err) != codes.NotFound {
		return fmt.Errorf("release document file: %w", err)
	}

	return nil
}

// Discard releases the file and deletes it.
func (m *MediaDocuments) Discard(ctx context.Context, mediaID string) error {
	if err := m.Release(ctx, mediaID); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, mediaCallTimeout)
	defer cancel()

	if _, err := m.media.DeleteMedia(ctx, &mediav1.DeleteMediaRequest{MediaId: mediaID}); err != nil &&
		status.Code(err) != codes.NotFound {
		return fmt.Errorf("delete document file: %w", err)
	}

	return nil
}

// DownloadURL is a short-lived link to a file this service holds.
func (m *MediaDocuments) DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error) {
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
