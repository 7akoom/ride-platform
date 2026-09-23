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

	response, handlerErr := handler(contextWithStaffCall(ctx), request)
	authorizer.Complete(ctx, auditEntryID, status.Code(handlerErr))

	return response, handlerErr
}

// staffTargetOf returns the id an admin request is about, for the audit log.
func staffTargetOf(request any) string {
	switch r := request.(type) {
	case interface{ GetMediaId() string }:
		return r.GetMediaId()
	default:
		return ""
	}
}

type staffCallContextKey struct{}

func contextWithStaffCall(ctx context.Context) context.Context {
	return context.WithValue(ctx, staffCallContextKey{}, true)
}

// isStaffCall reports whether the request was allowed through a staff permission
// (rather than the internal token or the caller's own ownership).
func isStaffCall(ctx context.Context) bool {
	called, _ := ctx.Value(staffCallContextKey{}).(bool)

	return called
}
