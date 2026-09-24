package grpc

import (
	"context"
	"errors"
	"testing"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type profileMapResolver struct {
	riders  map[string]string
	drivers map[string]string
	err     error
}

func (m profileMapResolver) RiderID(_ context.Context, identityID string) (string, error) {
	return m.riders[identityID], m.err
}

func (m profileMapResolver) DriverID(_ context.Context, identityID string) (string, error) {
	return m.drivers[identityID], m.err
}

var ownershipTestProfiles = profileMapResolver{
	riders:  map[string]string{"identity-rider-a": "rider-a", "identity-rider-b": "rider-b", "identity-both": "rider-both"},
	drivers: map[string]string{"identity-driver-a": "driver-a", "identity-driver-b": "driver-b", "identity-both": "driver-both"},
}

func callOwnershipAs(t *testing.T, identityID string, resolver CallerResolver, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(resolver, nil)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identityID,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

const walletRPCPrefix = "/ride.wallet.v1.WalletService/"

func TestWalletOwnersReachTheirOwnWallets(t *testing.T) {
	cases := []struct {
		name     string
		identity string
		method   string
		request  any
	}{
		{"rider reads own wallet", "identity-rider-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-a"}},
		{"driver reads own wallet", "identity-driver-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_DRIVER, OwnerId: "driver-a"}},
		{"rider lists own transactions", "identity-rider-a", "ListTransactions", &walletv1.ListTransactionsRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-a"}},
		{"driver lists own transactions", "identity-driver-a", "ListTransactions", &walletv1.ListTransactionsRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_DRIVER, OwnerId: "driver-a"}},
		{"driver checks own standing", "identity-driver-a", "CheckDriverStanding", &walletv1.CheckDriverStandingRequest{DriverId: "driver-a"}},
		{"driver requests own payout", "identity-driver-a", "RequestPayout", &walletv1.RequestPayoutRequest{DriverId: "driver-a"}},
		{"driver tops up own wallet", "identity-driver-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{DriverId: "driver-a"}},
		{"someone who is both reaches each own wallet", "identity-both", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_DRIVER, OwnerId: "driver-both"}},
		{"rider sends from own wallet", "identity-rider-a", "SendTransfer", &walletv1.SendTransferRequest{RiderId: "rider-a"}},
		{"rider lists own transfers", "identity-rider-a", "ListTransfers", &walletv1.ListTransfersRequest{RiderId: "rider-a"}},
	}

	for _, tc := range cases {
		if code := callOwnershipAs(t, tc.identity, ownershipTestProfiles, walletRPCPrefix+tc.method, tc.request); code != codes.OK {
			t.Errorf("%s: expected OK, got %v", tc.name, code)
		}
	}
}

func TestWalletNonOwnersAreDenied(t *testing.T) {
	cases := []struct {
		name     string
		identity string
		method   string
		request  any
	}{
		{"rider reads another rider's wallet", "identity-rider-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-b"}},
		{"rider reads a driver's wallet", "identity-rider-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_DRIVER, OwnerId: "driver-a"}},
		{"driver reads a rider's wallet", "identity-driver-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-a"}},
		{"driver reads another driver's wallet", "identity-driver-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_DRIVER, OwnerId: "driver-b"}},
		{"rider passes a driver id as the owner type rider", "identity-rider-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "driver-a"}},
		{"unspecified owner type", "identity-rider-a", "GetWallet", &walletv1.GetWalletRequest{OwnerId: "rider-a"}},
		{"empty owner id", "identity-rider-a", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER}},
		{"another rider's transactions", "identity-rider-a", "ListTransactions", &walletv1.ListTransactionsRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-b"}},
		{"another driver's standing", "identity-driver-a", "CheckDriverStanding", &walletv1.CheckDriverStandingRequest{DriverId: "driver-b"}},
		{"another driver's payout", "identity-driver-a", "RequestPayout", &walletv1.RequestPayoutRequest{DriverId: "driver-b"}},
		{"another driver's top-up", "identity-driver-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{DriverId: "driver-b"}},
		{"a rider requesting a payout", "identity-rider-a", "RequestPayout", &walletv1.RequestPayoutRequest{DriverId: "driver-a"}},
		{"a user with no profile at all", "identity-nobody", "GetWallet", &walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-a"}},
		{"a rider sends from another's wallet", "identity-rider-a", "SendTransfer", &walletv1.SendTransferRequest{RiderId: "rider-b"}},
		{"a driver sends from a driver wallet", "identity-driver-a", "SendTransfer", &walletv1.SendTransferRequest{RiderId: "driver-a"}},
		{"another rider's transfers", "identity-rider-a", "ListTransfers", &walletv1.ListTransfersRequest{RiderId: "rider-b"}},
		{"a wrong request type", "identity-rider-a", "GetWallet", &walletv1.TopUpRequest{}},
	}

	for _, tc := range cases {
		if code := callOwnershipAs(t, tc.identity, ownershipTestProfiles, walletRPCPrefix+tc.method, tc.request); code != codes.PermissionDenied {
			t.Errorf("%s: expected PermissionDenied, got %v", tc.name, code)
		}
	}
}

func TestWalletMoneyMovingRPCsStayInternalOnly(t *testing.T) {
	for _, method := range []string{"TopUp", "SettleTrip"} {
		if code := callOwnershipAs(t, "identity-rider-a", ownershipTestProfiles, walletRPCPrefix+method, &walletv1.TopUpRequest{OwnerId: "rider-a"}); code != codes.PermissionDenied {
			t.Errorf("%s: an end user must never reach it, got %v", method, code)
		}
	}
}

func TestWalletOwnershipIsUnavailableNotAllowedWhenProfilesCannotBeLoaded(t *testing.T) {
	broken := profileMapResolver{err: errors.New("rider-service down")}

	code := callOwnershipAs(t, "identity-rider-a", broken, walletRPCPrefix+"GetWallet",
		&walletv1.GetWalletRequest{OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-a"})
	if code != codes.Unavailable {
		t.Fatalf("expected Unavailable, got %v", code)
	}
}
