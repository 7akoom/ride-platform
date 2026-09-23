package grpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
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

const staffServicePrefix = "/ride.staff.v1.StaffService/"

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName: {},
}

// methodAccess classifies every RPC. Methods missing from it are denied to end
// users; accessStaff methods also need an entry in staffPermissions.
var methodAccess = map[string]accessLevel{
	staffServicePrefix + "GetMyStaffProfile": accessAuthenticated,
	staffServicePrefix + "AcceptStaffInvite": accessAuthenticated,

	staffServicePrefix + "ListStaffMembers":      accessStaff,
	staffServicePrefix + "GetStaffMember":        accessStaff,
	staffServicePrefix + "InviteStaffMember":     accessStaff,
	staffServicePrefix + "SetStaffRoles":         accessStaff,
	staffServicePrefix + "SuspendStaffMember":    accessStaff,
	staffServicePrefix + "ReactivateStaffMember": accessStaff,
	staffServicePrefix + "RevokeStaffInvite":     accessStaff,
	staffServicePrefix + "ListRoles":             accessStaff,
	staffServicePrefix + "CreateRole":            accessStaff,
	staffServicePrefix + "UpdateRole":            accessStaff,
	staffServicePrefix + "DeleteRole":            accessStaff,
	staffServicePrefix + "ListPermissions":       accessStaff,
	staffServicePrefix + "ListAuditEntries":      accessStaff,

	// Asked by every other service before an admin RPC runs. A user token must
	// never reach these: it could forge the audit log or ask about anyone.
	staffServicePrefix + "AuthorizeStaffAction": accessInternal,
	staffServicePrefix + "CompleteStaffAction":  accessInternal,
}

// staffPermissions names the permission each accessStaff method needs.
var staffPermissions = map[string]string{
	staffServicePrefix + "ListStaffMembers":      staff.PermissionStaffRead,
	staffServicePrefix + "GetStaffMember":        staff.PermissionStaffRead,
	staffServicePrefix + "InviteStaffMember":     staff.PermissionStaffManage,
	staffServicePrefix + "SetStaffRoles":         staff.PermissionStaffManage,
	staffServicePrefix + "SuspendStaffMember":    staff.PermissionStaffManage,
	staffServicePrefix + "ReactivateStaffMember": staff.PermissionStaffManage,
	staffServicePrefix + "RevokeStaffInvite":     staff.PermissionStaffManage,
	staffServicePrefix + "ListRoles":             staff.PermissionStaffRead,
	staffServicePrefix + "CreateRole":            staff.PermissionRolesManage,
	staffServicePrefix + "UpdateRole":            staff.PermissionRolesManage,
	staffServicePrefix + "DeleteRole":            staff.PermissionRolesManage,
	staffServicePrefix + "ListPermissions":       staff.PermissionStaffRead,
	staffServicePrefix + "ListAuditEntries":      staff.PermissionAuditRead,
}

// StaffAuthorizer decides and records staff actions.
type StaffAuthorizer interface {
	Authorize(ctx context.Context, input staff.AuthorizeInput) (staff.Authorization, error)
	CompleteAction(ctx context.Context, auditEntryID string, code string) error
}

type actorContextKey struct{}

func contextWithActor(ctx context.Context, actor staff.Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func actorFromContext(ctx context.Context) staff.Actor {
	actor, _ := ctx.Value(actorContextKey{}).(staff.Actor)

	return actor
}

func NewAuthorizationUnaryInterceptor(authorizer StaffAuthorizer, logger *slog.Logger) googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess, staffPermissions, authorizer, logger)
}

func newAuthorizationInterceptor(
	levels map[string]accessLevel,
	permissions map[string]string,
	authorizer StaffAuthorizer,
	logger *slog.Logger,
) googlegrpc.UnaryServerInterceptor {
	if authorizer == nil {
		panic("staff authorizer is required")
	}

	if logger == nil {
		panic("logger is required")
	}

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
			permission, registered := permissions[info.FullMethod]
			if !registered {
				break
			}

			decision, err := authorizer.Authorize(ctx, staff.AuthorizeInput{
				IdentityID: principal.IdentityID,
				Permission: permission,
				Method:     info.FullMethod,
				TargetID:   targetOf(request),
			})
			if err != nil {
				logger.Error("staff authorization failed", "method", info.FullMethod, "error", err)

				return nil, status.Error(codes.Unavailable, "permission could not be verified")
			}

			if !decision.Allowed {
				break
			}

			response, handlerErr := handler(contextWithActor(ctx, decision.Actor), request)
			completeAction(authorizer, logger, decision.AuditEntryID, handlerErr)

			return response, handlerErr
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}

// completeAction records how the action ended. The action already happened, so
// a failure here is logged and never turned into an error for the caller.
func completeAction(authorizer StaffAuthorizer, logger *slog.Logger, auditEntryID string, handlerErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := authorizer.CompleteAction(ctx, auditEntryID, status.Code(handlerErr).String()); err != nil {
		logger.Warn("failed to record the outcome of a staff action", "audit_entry_id", auditEntryID, "error", err)
	}
}

// targetOf returns the id the request is about, when it names one.
func targetOf(request any) string {
	switch r := request.(type) {
	case interface{ GetStaffId() string }:
		return r.GetStaffId()
	case interface{ GetRoleId() string }:
		return r.GetRoleId()
	case interface{ GetEmail() string }:
		return r.GetEmail()
	default:
		return ""
	}
}
