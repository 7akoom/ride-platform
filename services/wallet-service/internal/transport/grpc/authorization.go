package grpc

import (
	"context"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type accessLevel int

const (
	accessInternal accessLevel = iota + 1
	accessOwner
	accessAuthenticated
	// accessStaff methods are for staff holding the permission named in
	// staffPermissions (and for the internal token).
	accessStaff
)

type ownerCheck func(ctx context.Context, c caller, request any) (bool, error)

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName:                   {},
	"/ride.wallet.v1.WalletService/ProcessZainCashWebhook": {},
}

// methodAccess classifies every RPC. Methods missing from it are denied to
// end users, and so are accessOwner methods without an entry in ownerChecks.
var methodAccess = map[string]accessLevel{
	"/ride.wallet.v1.WalletService/GetWallet":           accessOwner,
	"/ride.wallet.v1.WalletService/TopUp":               accessInternal,
	"/ride.wallet.v1.WalletService/SettleTrip":          accessInternal,
	"/ride.wallet.v1.WalletService/ListTransactions":    accessOwner,
	"/ride.wallet.v1.WalletService/CheckDriverStanding": accessOwner,
	"/ride.wallet.v1.WalletService/RequestPayout":       accessOwner,
	"/ride.wallet.v1.WalletService/InitiateTopUp":       accessOwner,
	"/ride.wallet.v1.WalletService/GetTripSettlement":   accessOwner,
	"/ride.wallet.v1.WalletService/RecordTripChange":    accessOwner,
	"/ride.wallet.v1.WalletService/SendTransfer":        accessOwner,
	"/ride.wallet.v1.WalletService/ListTransfers":       accessOwner,
	"/ride.wallet.v1.WalletService/CreateMoneyRequest":  accessOwner,
	"/ride.wallet.v1.WalletService/ListMoneyRequests":   accessOwner,
	"/ride.wallet.v1.WalletService/GetMoneyRequest":     accessOwner,
	"/ride.wallet.v1.WalletService/PayMoneyRequest":     accessOwner,
	"/ride.wallet.v1.WalletService/DeclineMoneyRequest": accessOwner,
	"/ride.wallet.v1.WalletService/CancelMoneyRequest":  accessOwner,
	"/ride.wallet.v1.WalletService/GetStatement":        accessOwner,
	"/ride.wallet.v1.WalletService/GetRiderDues":        accessOwner,
	"/ride.wallet.v1.WalletService/RedeemVoucher":       accessOwner,
	"/ride.wallet.v1.WalletService/ListPayouts":         accessOwner,

	"/ride.wallet.v1.WalletService/CreateVoucherBatch": accessStaff,
	"/ride.wallet.v1.WalletService/ListVoucherBatches": accessStaff,
	"/ride.wallet.v1.WalletService/GetVoucherBatch":    accessStaff,
	"/ride.wallet.v1.WalletService/ExportVoucherBatch": accessStaff,
	"/ride.wallet.v1.WalletService/CancelVoucherBatch": accessStaff,
	"/ride.wallet.v1.WalletService/GetVoucher":         accessStaff,
	"/ride.wallet.v1.WalletService/VoidVoucher":        accessStaff,

	"/ride.wallet.v1.WalletService/InspectWallet":        accessStaff,
	"/ride.wallet.v1.WalletService/GetStatementForStaff": accessStaff,
	"/ride.wallet.v1.WalletService/AdjustWallet":         accessStaff,
	"/ride.wallet.v1.WalletService/RefundTrip":           accessStaff,
	"/ride.wallet.v1.WalletService/ListTripRefunds":      accessStaff,
	"/ride.wallet.v1.WalletService/ListPayoutRequests":   accessStaff,
	"/ride.wallet.v1.WalletService/ApprovePayout":        accessStaff,
	"/ride.wallet.v1.WalletService/MarkPayoutPaid":       accessStaff,
	"/ride.wallet.v1.WalletService/RejectPayout":         accessStaff,
}

// staffPermissions names the staff permission for every accessStaff method.
// Vouchers are one permission: whoever may issue them may also see, export,
// cancel and void them. Looking at wallets is apart from moving money in
// them; the payout queue is its own.
var staffPermissions = map[string]string{
	"/ride.wallet.v1.WalletService/CreateVoucherBatch": permissionVouchersManage,
	"/ride.wallet.v1.WalletService/ListVoucherBatches": permissionVouchersManage,
	"/ride.wallet.v1.WalletService/GetVoucherBatch":    permissionVouchersManage,
	"/ride.wallet.v1.WalletService/ExportVoucherBatch": permissionVouchersManage,
	"/ride.wallet.v1.WalletService/CancelVoucherBatch": permissionVouchersManage,
	"/ride.wallet.v1.WalletService/GetVoucher":         permissionVouchersManage,
	"/ride.wallet.v1.WalletService/VoidVoucher":        permissionVouchersManage,

	"/ride.wallet.v1.WalletService/InspectWallet":        permissionWalletsRead,
	"/ride.wallet.v1.WalletService/GetStatementForStaff": permissionWalletsRead,
	"/ride.wallet.v1.WalletService/ListTripRefunds":      permissionWalletsRead,
	"/ride.wallet.v1.WalletService/AdjustWallet":         permissionWalletsAdjust,
	"/ride.wallet.v1.WalletService/RefundTrip":           permissionWalletsAdjust,
	"/ride.wallet.v1.WalletService/ListPayoutRequests":   permissionPayoutsManage,
	"/ride.wallet.v1.WalletService/ApprovePayout":        permissionPayoutsManage,
	"/ride.wallet.v1.WalletService/MarkPayoutPaid":       permissionPayoutsManage,
	"/ride.wallet.v1.WalletService/RejectPayout":         permissionPayoutsManage,
}

const (
	permissionVouchersManage = "vouchers.manage"
	permissionWalletsRead    = "wallets.read"
	permissionWalletsAdjust  = "wallets.adjust"
	permissionPayoutsManage  = "payouts.manage"
)

func NewAuthorizationUnaryInterceptor(resolver CallerResolver, staff StaffAuthorizer) googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess, ownerChecks, staffPermissions, resolver, staff)
}

func newAuthorizationInterceptor(
	levels map[string]accessLevel,
	checks map[string]ownerCheck,
	permissions map[string]string,
	resolver CallerResolver,
	staff StaffAuthorizer,
) googlegrpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *googlegrpc.UnaryServerInfo,
		handler googlegrpc.UnaryHandler,
	) (any, error) {
		if info == nil {
			return nil, status.Error(codes.Internal, "gRPC method information is required")
		}

		if _, exempt := exemptMethods[info.FullMethod]; exempt {
			return handler(ctx, request)
		}

		principal, ok := authenticatedPrincipalFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "valid access token is required")
		}

		if principal.IdentityID == internalServicePrincipalID {
			return handler(ctx, request)
		}

		switch levels[info.FullMethod] {
		case accessAuthenticated:
			return handler(ctx, request)

		case accessOwner:
			check, registered := checks[info.FullMethod]
			if !registered || resolver == nil {
				break
			}

			allowed, err := check(ctx, caller{identityID: principal.IdentityID, resolver: resolver}, request)
			if err != nil {
				return nil, status.Error(codes.Unavailable, "ownership could not be verified")
			}

			if allowed {
				return handler(ctx, request)
			}

		case accessStaff:
			return runAsStaff(ctx, staff, principal.IdentityID, permissions[info.FullMethod], info, request, handler)
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}
