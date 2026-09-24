package grpc

import (
	"context"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
)

var ownerChecks = map[string]ownerCheck{
	"/ride.wallet.v1.WalletService/GetWallet": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.GetWalletRequest)
		if !ok {
			return false, nil
		}

		return ownsWallet(ctx, c, r.GetOwnerType(), r.GetOwnerId())
	},
	"/ride.wallet.v1.WalletService/ListTransactions": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.ListTransactionsRequest)
		if !ok {
			return false, nil
		}

		return ownsWallet(ctx, c, r.GetOwnerType(), r.GetOwnerId())
	},
	"/ride.wallet.v1.WalletService/CheckDriverStanding": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.CheckDriverStandingRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
	"/ride.wallet.v1.WalletService/RequestPayout": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.RequestPayoutRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
	"/ride.wallet.v1.WalletService/InitiateTopUp": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.InitiateTopUpRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
	"/ride.wallet.v1.WalletService/GetTripSettlement": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.GetTripSettlementRequest)
		if !ok {
			return false, nil
		}

		// Owning the wallet named in the request is only the first half: the
		// service then checks that the trip is one of that wallet owner's.
		return ownsWallet(ctx, c, r.GetOwnerType(), r.GetOwnerId())
	},
	"/ride.wallet.v1.WalletService/RecordTripChange": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.RecordTripChangeRequest)
		if !ok {
			return false, nil
		}

		// Only a driver reports change, and only as themselves. That the trip is theirs is
		// checked by the service against the settlement.
		return c.ownsDriver(ctx, r.GetDriverId())
	},
	"/ride.wallet.v1.WalletService/SendTransfer": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.SendTransferRequest)
		if !ok {
			return false, nil
		}

		// Only a rider sends, and only from their own wallet.
		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/ListTransfers": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.ListTransfersRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/CreateMoneyRequest": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.CreateMoneyRequestRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/ListMoneyRequests": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.ListMoneyRequestsRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/GetMoneyRequest": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.GetMoneyRequestRequest)
		if !ok {
			return false, nil
		}

		// Seen as the caller's own rider; whether that rider may see the
		// request is the service's check.
		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/PayMoneyRequest": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.PayMoneyRequestRequest)
		if !ok {
			return false, nil
		}

		// Paid only from the caller's own wallet.
		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/DeclineMoneyRequest": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.CloseMoneyRequestRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/CancelMoneyRequest": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.CloseMoneyRequestRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/GetStatement": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.GetStatementRequest)
		if !ok {
			return false, nil
		}

		return ownsWallet(ctx, c, r.GetOwnerType(), r.GetOwnerId())
	},
	"/ride.wallet.v1.WalletService/GetRiderDues": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.GetRiderDuesRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/RedeemVoucher": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.RedeemVoucherRequest)
		if !ok {
			return false, nil
		}

		// A rider redeems into their own wallet only.
		return c.ownsRider(ctx, r.GetRiderId())
	},
	"/ride.wallet.v1.WalletService/ListPayouts": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*walletv1.ListPayoutsRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
}

func ownsWallet(ctx context.Context, c caller, ownerType walletv1.OwnerType, ownerID string) (bool, error) {
	switch ownerType {
	case walletv1.OwnerType_OWNER_TYPE_RIDER:
		return c.ownsRider(ctx, ownerID)
	case walletv1.OwnerType_OWNER_TYPE_DRIVER:
		return c.ownsDriver(ctx, ownerID)
	default:
		return false, nil
	}
}
