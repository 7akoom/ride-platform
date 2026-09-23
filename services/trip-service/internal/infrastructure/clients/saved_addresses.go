package clients

import (
	"context"
	"fmt"
	"time"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const lookupTimeout = 5 * time.Second

// AddressBook reads a rider's saved addresses from rider-service, as a
// service (the connection carries the internal token).
type AddressBook struct {
	riders riderv1.RiderServiceClient
}

var _ trip.AddressBook = (*AddressBook)(nil)

func NewAddressBook(riderConn grpc.ClientConnInterface) *AddressBook {
	if riderConn == nil {
		panic("rider-service connection is required")
	}

	return &AddressBook{riders: riderv1.NewRiderServiceClient(riderConn)}
}

func (b *AddressBook) SavedAddress(ctx context.Context, riderID, addressID string) (trip.SavedAddress, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := b.riders.GetSavedAddress(ctx, &riderv1.GetSavedAddressRequest{RiderId: riderID, AddressId: addressID})

	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound, codes.InvalidArgument:
		return trip.SavedAddress{}, trip.ErrSavedAddressNotFound
	default:
		return trip.SavedAddress{}, fmt.Errorf("%w: rider-service GetSavedAddress: %v", trip.ErrUpstreamUnavailable, err)
	}

	a := response.GetAddress()

	return trip.SavedAddress{
		Coordinates:  trip.Coordinates{Latitude: a.GetCoordinates().GetLatitude(), Longitude: a.GetCoordinates().GetLongitude()},
		Address:      a.GetAddress(),
		Details:      a.GetDetails(),
		Note:         a.GetNoteForDriver(),
		PhotoMediaID: a.GetPhotoMediaId(),
	}, nil
}

// PhotoLinks asks media-service for short-lived download links, as a
// service: the photo is the rider's, the driver of their trip may see it.
type PhotoLinks struct {
	media mediav1.MediaServiceClient
}

var _ trip.PhotoLinks = (*PhotoLinks)(nil)

func NewPhotoLinks(mediaConn grpc.ClientConnInterface) *PhotoLinks {
	if mediaConn == nil {
		panic("media-service connection is required")
	}

	return &PhotoLinks{media: mediav1.NewMediaServiceClient(mediaConn)}
}

func (l *PhotoLinks) DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := l.media.GetDownloadURL(ctx, &mediav1.GetDownloadURLRequest{MediaId: mediaID})

	switch status.Code(err) {
	case codes.OK:
		return response.GetUrl(), response.GetExpiresAt().AsTime(), nil
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument:
		// Deleted with its saved address since the trip was requested.
		return "", time.Time{}, trip.ErrNoPickupPhoto
	default:
		return "", time.Time{}, fmt.Errorf("%w: media-service GetDownloadURL: %v", trip.ErrUpstreamUnavailable, err)
	}
}
