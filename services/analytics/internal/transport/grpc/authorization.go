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
	accessAuthenticated
	accessStaff
)

const permissionAnalyticsRead = "analytics.read"

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName: {},
}

// methodAccess classifies every RPC; a method missing from it is denied to
// end users.
var methodAccess = map[string]accessLevel{
	"/ride.analytics.v1.AnalyticsService/GetTripFunnel":            accessStaff,
	"/ride.analytics.v1.AnalyticsService/GetCancellationBreakdown": accessStaff,
	"/ride.analytics.v1.AnalyticsService/GetRevenueSummary":        accessStaff,
	"/ride.analytics.v1.AnalyticsService/GetRiderRetention":        accessStaff,
	"/ride.analytics.v1.AnalyticsService/GetDriverRetention":       accessStaff,
	"/ride.analytics.v1.AnalyticsService/HealthCheck":              accessAuthenticated,
}

// staffPermissions names the permission each staff RPC needs.
var staffPermissions = map[string]string{
	"/ride.analytics.v1.AnalyticsService/GetTripFunnel":            permissionAnalyticsRead,
	"/ride.analytics.v1.AnalyticsService/GetCancellationBreakdown": permissionAnalyticsRead,
	"/ride.analytics.v1.AnalyticsService/GetRevenueSummary":        permissionAnalyticsRead,
	"/ride.analytics.v1.AnalyticsService/GetRiderRetention":        permissionAnalyticsRead,
	"/ride.analytics.v1.AnalyticsService/GetDriverRetention":       permissionAnalyticsRead,
}

func NewAuthorizationUnaryInterceptor(staff StaffAuthorizer) googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess, staffPermissions, staff)
}

func newAuthorizationInterceptor(
	levels map[string]accessLevel,
	permissions map[string]string,
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
		case accessStaff:
			return runAsStaff(ctx, staff, principal.IdentityID, permissions[info.FullMethod], info, request, handler)
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}
