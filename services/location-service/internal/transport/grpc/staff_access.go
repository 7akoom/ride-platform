package grpc

import (
	"context"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StaffAuthorizer asks staff-service whether a staff member may use a
// permission. staff-service records the attempt before it answers, allowed or
// not; Complete records how an allowed action ended.
type StaffAuthorizer interface {
	Authorize(ctx context.Context, identityID, permission, method, targetID string) (allowed bool, auditEntryID string, err error)
	Complete(ctx context.Context, auditEntryID string, code codes.Code)
}

// runAsStaff runs handler only if staff-service allows the caller the
// permission. It fails closed: no authorizer, no permission named, or no
// answer from staff-service all deny.
func runAsStaff(
	ctx context.Context,
	authorizer StaffAuthorizer,
	identityID string,
	permission string,
	info *googlegrpc.UnaryServerInfo,
	request any,
	handler googlegrpc.UnaryHandler,
) (any, error) {
	if authorizer == nil || permission == "" {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}

	allowed, auditEntryID, err := authorizer.Authorize(ctx, identityID, permission, info.FullMethod, staffTargetOf(request))
	if err != nil {
		return nil, status.Error(codes.Unavailable, "permission could not be verified")
	}

	if !allowed {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}

	response, handlerErr := handler(ctx, request)
	authorizer.Complete(ctx, auditEntryID, status.Code(handlerErr))

	return response, handlerErr
}

// staffTargetOf returns the id an admin request is about, for the audit log.
func staffTargetOf(request any) string {
	switch r := request.(type) {
	case interface{ GetZoneId() string }:
		return r.GetZoneId()
	default:
		return ""
	}
}
