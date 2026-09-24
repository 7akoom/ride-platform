package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/tips"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestTopUpsAndTipsReachOnlyTheCallersOwnWallet(t *testing.T) {
	rider := walletv1.OwnerType_OWNER_TYPE_RIDER
	driver := walletv1.OwnerType_OWNER_TYPE_DRIVER

	for _, c := range []struct {
		name     string
		identity string
		method   string
		request  any
		want     codes.Code
	}{
		{"a driver tops up (the older form)", "identity-driver-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{DriverId: "driver-a"}, codes.OK},
		{"another driver's (the older form)", "identity-driver-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{DriverId: "driver-b"}, codes.PermissionDenied},
		{"a rider tops up their own", "identity-rider-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{OwnerType: rider, OwnerId: "rider-a"}, codes.OK},
		{"a rider tops up another rider's", "identity-rider-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{OwnerType: rider, OwnerId: "rider-b"}, codes.PermissionDenied},
		{"the owner fields win over driver_id", "identity-driver-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{DriverId: "driver-a", OwnerType: driver, OwnerId: "driver-b"}, codes.PermissionDenied},
		{"a rider names a driver wallet with their id", "identity-rider-a", "InitiateTopUp", &walletv1.InitiateTopUpRequest{OwnerType: driver, OwnerId: "rider-a"}, codes.PermissionDenied},
		{"a rider reads their top-up", "identity-rider-a", "GetTopUp", &walletv1.GetTopUpRequest{OwnerType: rider, OwnerId: "rider-a"}, codes.OK},
		{"another rider's top-up", "identity-rider-a", "GetTopUp", &walletv1.GetTopUpRequest{OwnerType: rider, OwnerId: "rider-b"}, codes.PermissionDenied},
		{"a rider tips", "identity-rider-a", "TipDriver", &walletv1.TipDriverRequest{RiderId: "rider-a"}, codes.OK},
		{"from another rider's wallet", "identity-rider-a", "TipDriver", &walletv1.TipDriverRequest{RiderId: "rider-b"}, codes.PermissionDenied},
		{"a driver tips", "identity-driver-a", "TipDriver", &walletv1.TipDriverRequest{RiderId: "driver-a"}, codes.PermissionDenied},
	} {
		if got := callOwnershipAs(t, c.identity, ownershipTestProfiles, walletRPCPrefix+c.method, c.request); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTheOlderTopUpFormIsADriversOwn(t *testing.T) {
	ownerType, ownerID := topUpOwner(&walletv1.InitiateTopUpRequest{DriverId: "driver-a"})
	if ownerType != wallet.OwnerDriver || ownerID != "driver-a" {
		t.Fatalf("got %s %s", ownerType, ownerID)
	}

	ownerType, ownerID = topUpOwner(&walletv1.InitiateTopUpRequest{DriverId: "driver-a", OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: "rider-a"})
	if ownerType != wallet.OwnerRider || ownerID != "rider-a" {
		t.Fatalf("got %s %s", ownerType, ownerID)
	}
}

func TestTipAndTopUpErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		tips.ErrTripNotFound:        codes.NotFound,
		tips.ErrNotTippable:         codes.FailedPrecondition,
		tips.ErrAlreadyTipped:       codes.FailedPrecondition,
		tips.ErrBelowMin:            codes.FailedPrecondition,
		tips.ErrAboveMax:            codes.FailedPrecondition,
		tips.ErrKeyReused:           codes.AlreadyExists,
		tips.ErrInvalidAmount:       codes.InvalidArgument,
		wallet.ErrInsufficientFunds: codes.FailedPrecondition,
	} {
		if got := status.Code(handler.mapTipError(err)); got != want {
			t.Errorf("tip %v: got %v, want %v", err, got, want)
		}
	}

	for err, want := range map[error]codes.Code{
		topup.ErrTopUpNotFound:      codes.NotFound,
		topup.ErrBelowMinimum:       codes.FailedPrecondition,
		topup.ErrAboveMaximum:       codes.FailedPrecondition,
		topup.ErrAmountNotSupported: codes.FailedPrecondition,
		topup.ErrUnknownProvider:    codes.InvalidArgument,
		topup.ErrOwnerRequired:      codes.InvalidArgument,
	} {
		if got := status.Code(handler.mapTopUpError(err)); got != want {
			t.Errorf("top-up %v: got %v, want %v", err, got, want)
		}
	}

	if _, err := (&WalletHandler{}).TipDriver(context.Background(), &walletv1.TipDriverRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("without tips: %v", err)
	}
}
