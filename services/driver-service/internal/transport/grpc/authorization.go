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
var methodAccess = map[string]accessLevel{
	"/ride.driver.v1.DriverService/CreateDriver":        accessOwner,
	"/ride.driver.v1.DriverService/GetDriver":           accessOwner,
	"/ride.driver.v1.DriverService/GetDriverByIdentity": accessOwner,
	"/ride.driver.v1.DriverService/UpdateDriverProfile": accessOwner,
	"/ride.driver.v1.DriverService/UpdateAvailability":  accessOwner,

	// Operator actions: staff with the permission, or the internal token.
	// A driver approving themselves is the exact thing this blocks.
	"/ride.driver.v1.DriverService/ApproveDriver": accessStaff,
	"/ride.driver.v1.DriverService/RejectDriver":  accessStaff,
	"/ride.driver.v1.DriverService/ListDrivers":   accessStaff,
}

// staffPermissions names the staff permission for every accessStaff method,
// and for accessOwner methods staff may also use on someone else's data.
var staffPermissions = map[string]string{
	"/ride.driver.v1.DriverService/ApproveDriver": "drivers.approve",
	"/ride.driver.v1.DriverService/RejectDriver":  "drivers.approve",
	"/ride.driver.v1.DriverService/ListDrivers":   "drivers.read",
	"/ride.driver.v1.DriverService/GetDriver":     "drivers.read",
}

func NewAuthorizationUnaryInterceptor(drivers DriverReader, staff StaffAuthorizer) googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess, ownerChecks, staffPermissions, drivers, staff)
}

func newAuthorizationInterceptor(
	levels map[string]accessLevel,
	checks map[string]ownerCheck,
	permissions map[string]string,
	drivers DriverReader,
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

			allowed, err := check(ctx, caller{identityID: principal.IdentityID, drivers: drivers}, request)
			if err != nil {
				return nil, status.Error(codes.Unavailable, "ownership could not be verified")
			}

			if allowed {
				return handler(ctx, request)
			}

			if permission, staffMay := permissions[info.FullMethod]; staffMay {
				return runAsStaff(ctx, staff, principal.IdentityID, permission, info, request, handler)
			}

		case accessStaff:
			return runAsStaff(ctx, staff, principal.IdentityID, permissions[info.FullMethod], info, request, handler)
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}
