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
