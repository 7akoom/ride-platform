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
)

// ownerCheck reports whether the calling identity owns what the request
// touches. An error means ownership could not be verified.
type ownerCheck func(ctx context.Context, c caller, request any) (bool, error)

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName: {},
}

// methodAccess classifies every RPC. Methods missing from it are denied to
// end users, and so are accessOwner methods without an entry in ownerChecks.
var methodAccess = map[string]accessLevel{
	"/ride.rider.v1.RiderService/CreateRider":        accessOwner,
	"/ride.rider.v1.RiderService/GetRider":           accessOwner,
	"/ride.rider.v1.RiderService/GetRiderByIdentity": accessOwner,
	"/ride.rider.v1.RiderService/UpdateRiderProfile": accessOwner,

	"/ride.rider.v1.RiderService/CreateSavedAddress": accessOwner,
	"/ride.rider.v1.RiderService/ListSavedAddresses": accessOwner,
	"/ride.rider.v1.RiderService/GetSavedAddress":    accessOwner,
	"/ride.rider.v1.RiderService/UpdateSavedAddress": accessOwner,
	"/ride.rider.v1.RiderService/DeleteSavedAddress": accessOwner,
}

func NewAuthorizationUnaryInterceptor(riders RiderReader) googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess, ownerChecks, riders)
}

func newAuthorizationInterceptor(
	levels map[string]accessLevel,
	checks map[string]ownerCheck,
	riders RiderReader,
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

			allowed, err := check(ctx, caller{identityID: principal.IdentityID, riders: riders}, request)
			if err != nil {
				return nil, status.Error(codes.Unavailable, "ownership could not be verified")
			}

			if allowed {
				return handler(ctx, request)
			}
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}
