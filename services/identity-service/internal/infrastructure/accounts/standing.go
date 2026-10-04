// Package accounts asks the services that hold a person's profiles, trips and
// money what stands in the way of deleting the account, and erases its files.
// Every call carries the internal service token (the connections are dialled
// with it) and fails rather than guess: a service that does not answer makes
// the whole check fail with deletion.ErrUnavailable.
package accounts

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/deletion"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const callTimeout = 5 * time.Second

// Standing implements deletion.Accounts.
type Standing struct {
	riders  riderv1.RiderServiceClient
	drivers driverv1.DriverServiceClient
	trips   tripv1.TripServiceClient
	wallets walletv1.WalletServiceClient
}

func NewStanding(rider, driver, trip, wallet grpc.ClientConnInterface) *Standing {
	if rider == nil || driver == nil || trip == nil || wallet == nil {
		panic("rider, driver, trip and wallet connections are required")
	}

	return &Standing{
		riders:  riderv1.NewRiderServiceClient(rider),
		drivers: driverv1.NewDriverServiceClient(driver),
		trips:   tripv1.NewTripServiceClient(trip),
		wallets: walletv1.NewWalletServiceClient(wallet),
	}
}

func unavailable(what string, err error) error {
	return fmt.Errorf("%w: %s: %v", deletion.ErrUnavailable, what, err)
}

func (s *Standing) Standing(ctx context.Context, identityID string) (deletion.Standing, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*callTimeout)
	defer cancel()

	var out deletion.Standing

	rider, err := s.riders.GetRiderByIdentity(ctx, &riderv1.GetRiderByIdentityRequest{IdentityId: identityID})

	switch status.Code(err) {
	case codes.OK:
		out.RiderID = rider.GetRider().GetId()
	case codes.NotFound:
	default:
		return deletion.Standing{}, unavailable("rider profile", err)
	}

	driver, err := s.drivers.GetDriverByIdentity(ctx, &driverv1.GetDriverByIdentityRequest{IdentityId: identityID})

	switch status.Code(err) {
	case codes.OK:
		out.DriverID = driver.GetDriver().GetId()
	case codes.NotFound:
	default:
		return deletion.Standing{}, unavailable("driver profile", err)
	}

	add := func(b deletion.Blocker) {
		for _, existing := range out.Blockers {
			if existing == b {
				return
			}
		}

		out.Blockers = append(out.Blockers, b)
	}

	if out.RiderID != "" {
		if err := s.rider(ctx, out.RiderID, &out, add); err != nil {
			return deletion.Standing{}, err
		}
	}

	if out.DriverID != "" {
		if err := s.driver(ctx, out.DriverID, &out, add); err != nil {
			return deletion.Standing{}, err
		}
	}

	return out, nil
}

func (s *Standing) rider(ctx context.Context, riderID string, out *deletion.Standing, add func(deletion.Blocker)) error {
	if active, err := s.activeTrip(ctx, &tripv1.GetActiveTripRequest{RiderId: riderID}); err != nil {
		return err
	} else if active {
		add(deletion.BlockerActiveTrip)
	}

	scheduled, err := s.trips.ListScheduledTrips(ctx, &tripv1.ListScheduledTripsRequest{RiderId: riderID})
	if err != nil {
		return unavailable("scheduled trips", err)
	}

	for _, booking := range scheduled.GetScheduledTrips() {
		if booking.GetStatus() == "scheduled" {
			add(deletion.BlockerScheduledTrip)
		}
	}

	dues, err := s.wallets.GetRiderDues(ctx, &walletv1.GetRiderDuesRequest{RiderId: riderID})
	if err != nil {
		return unavailable("rider dues", err)
	}

	if sign(dues.GetOutstanding()) > 0 {
		add(deletion.BlockerUnpaidFees)
	}

	wallet, found, err := s.wallet(ctx, walletv1.OwnerType_OWNER_TYPE_RIDER, riderID)
	if err != nil {
		return err
	}

	if found && sign(wallet.GetBalance()) > 0 {
		out.Balances = append(out.Balances, deletion.Balance{
			OwnerType: "rider", Amount: wallet.GetBalance(), Currency: wallet.GetCurrencyCode(),
		})
	}

	return nil
}

func (s *Standing) driver(ctx context.Context, driverID string, out *deletion.Standing, add func(deletion.Blocker)) error {
	if active, err := s.activeTrip(ctx, &tripv1.GetActiveTripRequest{DriverId: driverID}); err != nil {
		return err
	} else if active {
		add(deletion.BlockerActiveTrip)
	}

	wallet, found, err := s.wallet(ctx, walletv1.OwnerType_OWNER_TYPE_DRIVER, driverID)
	if err != nil {
		return err
	}

	if found {
		switch sign(wallet.GetBalance()) {
		case -1:
			add(deletion.BlockerNegativeBalance)
		case 1:
			out.Balances = append(out.Balances, deletion.Balance{
				OwnerType: "driver", Amount: wallet.GetBalance(), Currency: wallet.GetCurrencyCode(),
			})
		}
	}

	payouts, err := s.wallets.ListPayouts(ctx, &walletv1.ListPayoutsRequest{DriverId: driverID, PageSize: 20})
	if err != nil {
		return unavailable("payouts", err)
	}

	for _, payout := range payouts.GetPayouts() {
		if payout.GetStatus() == "pending" || payout.GetStatus() == "approved" {
			add(deletion.BlockerOpenPayout)
		}
	}

	return nil
}

func (s *Standing) activeTrip(ctx context.Context, request *tripv1.GetActiveTripRequest) (bool, error) {
	_, err := s.trips.GetActiveTrip(ctx, request)

	switch status.Code(err) {
	case codes.OK:
		return true, nil
	case codes.NotFound:
		return false, nil
	default:
		return false, unavailable("active trip", err)
	}
}

func (s *Standing) wallet(ctx context.Context, owner walletv1.OwnerType, ownerID string) (*walletv1.Wallet, bool, error) {
	response, err := s.wallets.GetWallet(ctx, &walletv1.GetWalletRequest{OwnerType: owner, OwnerId: ownerID})

	switch status.Code(err) {
	case codes.OK:
		return response.GetWallet(), true, nil
	case codes.NotFound:
		return nil, false, nil
	default:
		return nil, false, unavailable("wallet", err)
	}
}

// sign is the sign of a decimal string; an empty or unreadable one is 0.
func sign(value string) int {
	parsed, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return 0
	}

	return parsed.Sign()
}

// Media erases an identity's files in media-service (deletion.Media).
type Media struct {
	client mediav1.MediaServiceClient
}

func NewMedia(conn grpc.ClientConnInterface) *Media {
	if conn == nil {
		panic("media connection is required")
	}

	return &Media{client: mediav1.NewMediaServiceClient(conn)}
}

func (m *Media) DeleteOwnerMedia(ctx context.Context, identityID string) error {
	ctx, cancel := context.WithTimeout(ctx, 6*callTimeout)
	defer cancel()

	if _, err := m.client.DeleteOwnerMedia(ctx, &mediav1.DeleteOwnerMediaRequest{OwnerIdentityId: identityID}); err != nil {
		return fmt.Errorf("delete the files in media-service: %w", err)
	}

	return nil
}
