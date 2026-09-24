package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestWalletOperationsNeedTheirOwnPermissions(t *testing.T) {
	for method, want := range map[string]struct {
		request    any
		permission string
	}{
		"InspectWallet":        {&walletv1.InspectWalletRequest{OwnerId: "rider-1"}, "wallets.read"},
		"GetStatementForStaff": {&walletv1.GetStatementRequest{OwnerId: "rider-1"}, "wallets.read"},
		"ListTripRefunds":      {&walletv1.ListTripRefundsRequest{TripId: "trip-1"}, "wallets.read"},
		"AdjustWallet":         {&walletv1.AdjustWalletRequest{OwnerId: "rider-1"}, "wallets.adjust"},
		"RefundTrip":           {&walletv1.RefundTripRequest{TripId: "trip-1"}, "wallets.adjust"},
		"ListPayoutRequests":   {&walletv1.ListPayoutRequestsRequest{}, "payouts.manage"},
		"ApprovePayout":        {&walletv1.ApprovePayoutRequest{PayoutId: "payout-1"}, "payouts.manage"},
		"MarkPayoutPaid":       {&walletv1.MarkPayoutPaidRequest{PayoutId: "payout-1"}, "payouts.manage"},
		"RejectPayout":         {&walletv1.RejectPayoutRequest{PayoutId: "payout-1"}, "payouts.manage"},
	} {
		denied := &fakeStaff{}
		if called, code := callAsStaff(denied, method, want.request); called || code != codes.PermissionDenied || denied.asked[0] != want.permission {
			t.Errorf("%s denied: called=%v %v asked %v", method, called, code, denied.asked)
		}

		allowed := &fakeStaff{allowed: true}
		if called, _ := callAsStaff(allowed, method, want.request); !called || allowed.targets[0] != staffTargetOf(want.request) || allowed.targets[0] == "" && method != "ListPayoutRequests" {
			t.Errorf("%s allowed: called=%v target %q", method, called, allowed.targets)
		}
	}
}

func TestADriverListsOnlyTheirOwnPayouts(t *testing.T) {
	method := walletRPCPrefix + "ListPayouts"

	if code := callOwnershipAs(t, "identity-driver-a", ownershipTestProfiles, method, &walletv1.ListPayoutsRequest{DriverId: "driver-a"}); code != codes.OK {
		t.Fatalf("own payouts: %v", code)
	}

	if code := callOwnershipAs(t, "identity-driver-a", ownershipTestProfiles, method, &walletv1.ListPayoutsRequest{DriverId: "driver-b"}); code != codes.PermissionDenied {
		t.Fatalf("another driver's: %v", code)
	}

	if code := callOwnershipAs(t, "identity-rider-a", ownershipTestProfiles, method, &walletv1.ListPayoutsRequest{DriverId: "driver-a"}); code != codes.PermissionDenied {
		t.Fatalf("a rider: %v", code)
	}
}

func TestTheInternalTokenMovesNoMoneyByHand(t *testing.T) {
	handler := (&WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).
		WithOperations(operations.NewService(emptyOperationsStore{}, emptyWallets{}))
	internal := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID})

	if _, err := handler.AdjustWallet(internal, &walletv1.AdjustWalletRequest{Amount: "100"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("adjust: %v", err)
	}

	if _, err := handler.RefundTrip(internal, &walletv1.RefundTripRequest{Amount: "100"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("refund: %v", err)
	}

	for name, call := range map[string]func() error{
		"approve": func() error { _, err := handler.ApprovePayout(internal, &walletv1.ApprovePayoutRequest{}); return err },
		"paid": func() error {
			_, err := handler.MarkPayoutPaid(internal, &walletv1.MarkPayoutPaidRequest{})
			return err
		},
		"reject": func() error { _, err := handler.RejectPayout(internal, &walletv1.RejectPayoutRequest{}); return err },
	} {
		if err := call(); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

type emptyOperationsStore struct{ operations.Store }

type emptyWallets struct{ wallet.Service }

func TestOperationsErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		operations.ErrPayoutNotFound:    codes.NotFound,
		operations.ErrTripNotSettled:    codes.NotFound,
		operations.ErrKeyReused:         codes.AlreadyExists,
		operations.ErrRefundTooLarge:    codes.FailedPrecondition,
		operations.ErrPayoutOpen:        codes.FailedPrecondition,
		operations.ErrPayoutState:       codes.FailedPrecondition,
		operations.ErrInvalidRefund:     codes.InvalidArgument,
		operations.ErrReasonRequired:    codes.InvalidArgument,
		operations.ErrReferenceRequired: codes.InvalidArgument,
		operations.ErrOwnerRequired:     codes.InvalidArgument,
		operations.ErrStaffRequired:     codes.PermissionDenied,
		wallet.ErrInsufficientFunds:     codes.FailedPrecondition,
		wallet.ErrWalletBlocked:         codes.FailedPrecondition,
		wallet.ErrBelowMinimumPayout:    codes.FailedPrecondition,
		wallet.ErrInvalidAmount:         codes.InvalidArgument,
	} {
		if got := status.Code(handler.mapOperationsError(err)); got != want {
			t.Errorf("%v: got %v, want %v", err, got, want)
		}
	}
}
