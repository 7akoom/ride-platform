package clients

import (
	"context"
	"fmt"
	"strings"
	"time"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

func required(conn grpc.ClientConnInterface, name string) {
	if conn == nil {
		panic(name + " connection is required")
	}
}

// Trips reads trips from trip-service, as a service.
type Trips struct{ trips tripv1.TripServiceClient }

var _ support.Trips = (*Trips)(nil)

func NewTrips(conn grpc.ClientConnInterface) *Trips {
	required(conn, "trip-service")

	return &Trips{trips: tripv1.NewTripServiceClient(conn)}
}

var tripStatuses = map[tripv1.TripStatus]string{
	tripv1.TripStatus_TRIP_STATUS_REQUESTED:   "requested",
	tripv1.TripStatus_TRIP_STATUS_ACCEPTED:    "accepted",
	tripv1.TripStatus_TRIP_STATUS_IN_PROGRESS: "in_progress",
	tripv1.TripStatus_TRIP_STATUS_COMPLETED:   "completed",
	tripv1.TripStatus_TRIP_STATUS_CANCELLED:   "cancelled",
}

func (t *Trips) GetTrip(ctx context.Context, tripID string) (support.TripInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	response, err := t.trips.GetTrip(ctx, &tripv1.GetTripRequest{TripId: tripID})
	if status.Code(err) == codes.NotFound || status.Code(err) == codes.InvalidArgument {
		return support.TripInfo{}, support.ErrNotFound
	}

	if err != nil {
		return support.TripInfo{}, unavailable("read trip", err)
	}

	trip := response.GetTrip()

	return support.TripInfo{
		ID:          trip.GetId(),
		RiderID:     trip.GetRiderId(),
		DriverID:    trip.GetDriverId(),
		Status:      tripStatuses[trip.GetStatus()],
		RequestedAt: trip.GetRequestedAt().AsTime(),
	}, nil
}

// Profiles finds rider and driver profiles, as a service.
type Profiles struct {
	riders  riderv1.RiderServiceClient
	drivers driverv1.DriverServiceClient
}

var _ support.Profiles = (*Profiles)(nil)

func NewProfiles(riderConn, driverConn grpc.ClientConnInterface) *Profiles {
	required(riderConn, "rider-service")
	required(driverConn, "driver-service")

	return &Profiles{riders: riderv1.NewRiderServiceClient(riderConn), drivers: driverv1.NewDriverServiceClient(driverConn)}
}

func (p *Profiles) ProfileIDByIdentity(ctx context.Context, audience support.Audience, identityID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var id string
	var err error

	if audience == support.AudienceRider {
		var response *riderv1.GetRiderResponse
		response, err = p.riders.GetRiderByIdentity(ctx, &riderv1.GetRiderByIdentityRequest{IdentityId: identityID})
		id = response.GetRider().GetId()
	} else {
		var response *driverv1.GetDriverResponse
		response, err = p.drivers.GetDriverByIdentity(ctx, &driverv1.GetDriverByIdentityRequest{IdentityId: identityID})
		id = response.GetDriver().GetId()
	}

	if status.Code(err) == codes.NotFound || (err == nil && id == "") {
		return "", support.ErrNoProfile
	}

	if err != nil {
		return "", unavailable("find "+string(audience)+" profile", err)
	}

	return id, nil
}

func (p *Profiles) IdentityByProfile(ctx context.Context, audience support.Audience, profileID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var identityID string
	var err error

	if audience == support.AudienceRider {
		var response *riderv1.GetRiderResponse
		response, err = p.riders.GetRider(ctx, &riderv1.GetRiderRequest{RiderId: profileID})
		identityID = response.GetRider().GetIdentityId()
	} else {
		var response *driverv1.GetDriverResponse
		response, err = p.drivers.GetDriver(ctx, &driverv1.GetDriverRequest{DriverId: profileID})
		identityID = response.GetDriver().GetIdentityId()
	}

	if status.Code(err) == codes.NotFound || (err == nil && identityID == "") {
		return "", fmt.Errorf("%w: no such %s", support.ErrActionNotAllowed, audience)
	}

	if err != nil {
		return "", unavailable("read "+string(audience)+" profile", err)
	}

	return identityID, nil
}

func (p *Profiles) SetDriverOffline(ctx context.Context, driverID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	_, err := p.drivers.UpdateAvailability(ctx, &driverv1.UpdateAvailabilityRequest{
		DriverId:           driverID,
		AvailabilityStatus: driverv1.AvailabilityStatus_AVAILABILITY_STATUS_OFFLINE,
	})

	return err
}

// Media keeps attachment files in media-service, as a service.
type Media struct{ media mediav1.MediaServiceClient }

var _ support.Media = (*Media)(nil)

func NewMedia(conn grpc.ClientConnInterface) *Media {
	required(conn, "media-service")

	return &Media{media: mediav1.NewMediaServiceClient(conn)}
}

func (m *Media) Hold(ctx context.Context, mediaID, ownerIdentityID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	_, err := m.media.HoldMedia(ctx, &mediav1.HoldMediaRequest{
		MediaId:         mediaID,
		OwnerIdentityId: ownerIdentityID,
		Purpose:         mediav1.MediaPurpose_MEDIA_PURPOSE_SUPPORT_ATTACHMENT,
	})

	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument, codes.PermissionDenied:
		return support.ErrMediaNotUsable
	default:
		return unavailable("hold attachment", err)
	}
}

func (m *Media) Release(ctx context.Context, mediaID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := m.media.ReleaseMedia(ctx, &mediav1.ReleaseMediaRequest{MediaId: mediaID}); err != nil &&
		status.Code(err) != codes.NotFound {
		return fmt.Errorf("release attachment: %w", err)
	}

	return nil
}

func (m *Media) DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	response, err := m.media.GetDownloadURL(ctx, &mediav1.GetDownloadURLRequest{MediaId: mediaID})
	if status.Code(err) == codes.NotFound || status.Code(err) == codes.FailedPrecondition {
		return "", time.Time{}, support.ErrNotFound
	}

	if err != nil {
		return "", time.Time{}, unavailable("attachment link", err)
	}

	return response.GetUrl(), response.GetExpiresAt().AsTime(), nil
}

// Wallet moves money for support actions, as a service acting for the staff
// member support-service authorized.
type Wallet struct{ wallet walletv1.WalletServiceClient }

var _ support.Wallet = (*Wallet)(nil)

func NewWallet(conn grpc.ClientConnInterface) *Wallet {
	required(conn, "wallet-service")

	return &Wallet{wallet: walletv1.NewWalletServiceClient(conn)}
}

func (w *Wallet) RefundTrip(
	ctx context.Context,
	actingIdentityID, tripID string,
	amount, driverAmount decimal.Decimal,
	reason, idempotencyKey string,
) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	_, err := w.wallet.RefundTrip(ctx, &walletv1.RefundTripRequest{
		TripId:           tripID,
		Amount:           amount.StringFixed(2),
		DriverAmount:     driverAmount.StringFixed(2),
		Reason:           trimReason(reason),
		IdempotencyKey:   idempotencyKey,
		ActingIdentityId: actingIdentityID,
	})
	if err != nil {
		return refusalOrUnavailable("refund", err)
	}

	return nil
}

func (w *Wallet) Credit(
	ctx context.Context,
	actingIdentityID string,
	ownerType support.Audience,
	ownerID string,
	amount decimal.Decimal,
	reason, idempotencyKey string,
) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	owner := walletv1.OwnerType_OWNER_TYPE_RIDER
	if ownerType == support.AudienceDriver {
		owner = walletv1.OwnerType_OWNER_TYPE_DRIVER
	}

	_, err := w.wallet.AdjustWallet(ctx, &walletv1.AdjustWalletRequest{
		OwnerType:        owner,
		OwnerId:          ownerID,
		Amount:           amount.StringFixed(2),
		Reason:           trimReason(reason),
		IdempotencyKey:   idempotencyKey,
		ActingIdentityId: actingIdentityID,
	})
	if err != nil {
		return refusalOrUnavailable("wallet credit", err)
	}

	return nil
}

func (w *Wallet) Refundable(ctx context.Context, tripID string) (decimal.Decimal, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	response, err := w.wallet.ListTripRefunds(ctx, &walletv1.ListTripRefundsRequest{TripId: tripID})
	if status.Code(err) == codes.NotFound || status.Code(err) == codes.FailedPrecondition {
		return decimal.Zero, nil
	}

	if err != nil {
		return decimal.Zero, unavailable("trip refunds", err)
	}

	paid, err := decimal.NewFromString(orZero(response.GetPaidAmount()))
	if err != nil {
		return decimal.Zero, unavailable("trip refunds", err)
	}

	refunded, err := decimal.NewFromString(orZero(response.GetRefundedAmount()))
	if err != nil {
		return decimal.Zero, unavailable("trip refunds", err)
	}

	return paid.Sub(refunded), nil
}

func orZero(value string) string {
	if strings.TrimSpace(value) == "" {
		return "0"
	}

	return value
}

// The wallet takes reasons of 3-300 characters.
func trimReason(reason string) string {
	runes := []rune(reason)
	if len(runes) > 300 {
		return string(runes[:300])
	}

	return reason
}

// Accounts suspends and reactivates identities in identity-service.
type Accounts struct {
	accounts identityv1.IdentityAccountServiceClient
}

var _ support.Accounts = (*Accounts)(nil)

func NewAccounts(conn grpc.ClientConnInterface) *Accounts {
	required(conn, "identity-service")

	return &Accounts{accounts: identityv1.NewIdentityAccountServiceClient(conn)}
}

func (a *Accounts) Suspend(ctx context.Context, identityID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := a.accounts.SuspendIdentity(ctx, &identityv1.SuspendIdentityRequest{IdentityId: identityID}); err != nil {
		return refusalOrUnavailable("suspend", err)
	}

	return nil
}

func (a *Accounts) Reactivate(ctx context.Context, identityID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := a.accounts.ReactivateIdentity(ctx, &identityv1.ReactivateIdentityRequest{IdentityId: identityID}); err != nil {
		return refusalOrUnavailable("reactivate", err)
	}

	return nil
}
