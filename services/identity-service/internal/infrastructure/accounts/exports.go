package accounts

import (
	"context"
	"fmt"
	"time"

	dataexportv1 "github.com/7akoom/ride-platform/gen/go/ride/dataexport/v1"
	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	notificationv1 "github.com/7akoom/ride-platform/gen/go/ride/notification/v1"
	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/dataexport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxExportMessage is the largest answer one service may give for one person.
const maxExportMessage = 64 << 20

const exportCallTimeout = 2 * time.Minute

// Profiles finds the person's rider and driver profiles (dataexport.ProfileFinder).
func (s *Standing) Profiles(ctx context.Context, identityID string) (dataexport.Profiles, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*callTimeout)
	defer cancel()

	var out dataexport.Profiles

	rider, err := s.riders.GetRiderByIdentity(ctx, &riderv1.GetRiderByIdentityRequest{IdentityId: identityID})

	switch status.Code(err) {
	case codes.OK:
		out.RiderID = rider.GetRider().GetId()
	case codes.NotFound:
	default:
		return dataexport.Profiles{}, fmt.Errorf("rider profile: %w", err)
	}

	driver, err := s.drivers.GetDriverByIdentity(ctx, &driverv1.GetDriverByIdentityRequest{IdentityId: identityID})

	switch status.Code(err) {
	case codes.OK:
		out.DriverID = driver.GetDriver().GetId()
	case codes.NotFound:
	default:
		return dataexport.Profiles{}, fmt.Errorf("driver profile: %w", err)
	}

	return out, nil
}

type exportCall func(ctx context.Context, request *dataexportv1.ExportPersonalDataRequest, options ...grpc.CallOption) (*dataexportv1.ExportPersonalDataResponse, error)

func source(name string, call exportCall) dataexport.Source {
	return dataexport.Source{
		Name: name,
		Export: func(ctx context.Context, identityID string, profiles dataexport.Profiles) ([]dataexport.Section, error) {
			ctx, cancel := context.WithTimeout(ctx, exportCallTimeout)
			defer cancel()

			response, err := call(ctx, &dataexportv1.ExportPersonalDataRequest{
				IdentityId: identityID, RiderId: profiles.RiderID, DriverId: profiles.DriverID,
			}, grpc.MaxCallRecvMsgSize(maxExportMessage))
			if err != nil {
				return nil, err
			}

			out := make([]dataexport.Section, 0, len(response.GetSections()))
			for _, section := range response.GetSections() {
				out = append(out, dataexport.Section{Name: section.GetName(), Content: section.GetContent()})
			}

			return out, nil
		},
	}
}

// ExportConns are the services that keep data about people.
type ExportConns struct {
	Rider, Driver, Trip, Wallet, Support, Notification grpc.ClientConnInterface
}

// Sources asks each of them for its part of a person's export.
func Sources(conns ExportConns) []dataexport.Source {
	return []dataexport.Source{
		source("rider-service", riderv1.NewRiderServiceClient(conns.Rider).ExportPersonalData),
		source("driver-service", driverv1.NewDriverServiceClient(conns.Driver).ExportPersonalData),
		source("trip-service", tripv1.NewTripServiceClient(conns.Trip).ExportPersonalData),
		source("wallet-service", walletv1.NewWalletServiceClient(conns.Wallet).ExportPersonalData),
		source("support-service", supportv1.NewSupportServiceClient(conns.Support).ExportPersonalData),
		source("notification-service", notificationv1.NewNotificationServiceClient(conns.Notification).ExportPersonalData),
	}
}

// Store keeps the ZIP in media-service as the person's file (dataexport.Files).
func (m *Media) Store(ctx context.Context, ownerIdentityID string, zip []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, exportCallTimeout)
	defer cancel()

	response, err := m.client.StoreFile(ctx, &mediav1.StoreFileRequest{
		OwnerIdentityId: ownerIdentityID,
		Purpose:         mediav1.MediaPurpose_MEDIA_PURPOSE_DATA_EXPORT,
		ContentType:     "application/zip",
		Content:         zip,
	}, grpc.MaxCallSendMsgSize(maxExportMessage+(16<<20)))
	if err != nil {
		return "", fmt.Errorf("store the file in media-service: %w", err)
	}

	return response.GetMedia().GetId(), nil
}

func (m *Media) DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	response, err := m.client.GetDownloadURL(ctx, &mediav1.GetDownloadURLRequest{MediaId: mediaID})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("download link from media-service: %w", err)
	}

	return response.GetUrl(), response.GetExpiresAt().AsTime(), nil
}

func (m *Media) Delete(ctx context.Context, mediaID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	_, err := m.client.DeleteMedia(ctx, &mediav1.DeleteMediaRequest{MediaId: mediaID})
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("delete the file in media-service: %w", err)
	}

	return nil
}
