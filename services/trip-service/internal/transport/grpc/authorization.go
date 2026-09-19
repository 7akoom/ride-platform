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

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName: {},
}

// methodAccess classifies every RPC. Methods missing from it are denied to
// end users. accessOwner methods stay closed to end users until their
// handlers verify ownership.
var methodAccess = map[string]accessLevel{
	"/ride.trip.v1.TripService/RequestTrip":    accessOwner,
	"/ride.trip.v1.TripService/AcceptTrip":     accessOwner,
	"/ride.trip.v1.TripService/StartTrip":      accessOwner,
	"/ride.trip.v1.TripService/CompleteTrip":   accessOwner,
	"/ride.trip.v1.TripService/CancelTrip":     accessOwner,
	"/ride.trip.v1.TripService/GetTrip":        accessOwner,
	"/ride.trip.v1.TripService/TriggerSOS":     accessOwner,
	"/ride.trip.v1.TripService/RecordWaypoint": accessOwner,
	"/ride.trip.v1.TripService/GetTripPath":    accessOwner,
}

func NewAuthorizationUnaryInterceptor() googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess)
}

func newAuthorizationInterceptor(levels map[string]accessLevel) googlegrpc.UnaryServerInterceptor {
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

		if levels[info.FullMethod] == accessAuthenticated {
			return handler(ctx, request)
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}
