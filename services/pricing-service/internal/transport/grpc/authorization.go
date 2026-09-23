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

// ownerCheck reports whether the calling identity owns what the request
// touches. An error means ownership could not be verified.
type ownerCheck func(ctx context.Context, c caller, request any) (bool, error)

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName: {},
}

// methodAccess classifies every RPC. Methods missing from it are denied to
// end users, and so are accessOwner methods without an entry in ownerChecks.
//
// ClaimQuote and ReleaseQuote are trip-service's: a rider takes a quote by
// requesting a trip with it, never by claiming it directly.
var methodAccess = map[string]accessLevel{
	"/ride.pricing.v1.PricingService/EstimateFare":  accessOwner,
	"/ride.pricing.v1.PricingService/CalculateFare": accessInternal,
	"/ride.pricing.v1.PricingService/CreateCoupon":  accessInternal,
	"/ride.pricing.v1.PricingService/GetCoupon":     accessInternal,

	"/ride.pricing.v1.PricingService/QuoteTrip":    accessOwner,
	"/ride.pricing.v1.PricingService/ClaimQuote":   accessInternal,
	"/ride.pricing.v1.PricingService/ReleaseQuote": accessInternal,

	"/ride.pricing.v1.PricingService/ListRateCards":      accessStaff,
	"/ride.pricing.v1.PricingService/SetRateCard":        accessStaff,
	"/ride.pricing.v1.PricingService/RetireRateCard":     accessStaff,
	"/ride.pricing.v1.PricingService/ListSurgeRules":     accessStaff,
	"/ride.pricing.v1.PricingService/CreateSurgeRule":    accessStaff,
	"/ride.pricing.v1.PricingService/UpdateSurgeRule":    accessStaff,
	"/ride.pricing.v1.PricingService/SetSurgeRuleActive": accessStaff,
	"/ride.pricing.v1.PricingService/ListZoneSurges":     accessStaff,
	"/ride.pricing.v1.PricingService/CreateZoneSurge":    accessStaff,
	"/ride.pricing.v1.PricingService/EndZoneSurge":       accessStaff,
}

// staffPermissions names the staff permission for every accessStaff method.
// Prices are one permission: whoever may see them may also set them.
var staffPermissions = map[string]string{
	"/ride.pricing.v1.PricingService/ListRateCards":      permissionPricingManage,
	"/ride.pricing.v1.PricingService/SetRateCard":        permissionPricingManage,
	"/ride.pricing.v1.PricingService/RetireRateCard":     permissionPricingManage,
	"/ride.pricing.v1.PricingService/ListSurgeRules":     permissionPricingManage,
	"/ride.pricing.v1.PricingService/CreateSurgeRule":    permissionPricingManage,
	"/ride.pricing.v1.PricingService/UpdateSurgeRule":    permissionPricingManage,
	"/ride.pricing.v1.PricingService/SetSurgeRuleActive": permissionPricingManage,
	"/ride.pricing.v1.PricingService/ListZoneSurges":     permissionPricingManage,
	"/ride.pricing.v1.PricingService/CreateZoneSurge":    permissionPricingManage,
	"/ride.pricing.v1.PricingService/EndZoneSurge":       permissionPricingManage,
}

const permissionPricingManage = "pricing.manage"

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
			if !registered {
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
