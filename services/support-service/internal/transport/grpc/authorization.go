package grpc

import (
	"context"

	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

const supportRPCPrefix = "/ride.support.v1.SupportService/"

// userMethods are open to any signed-in person; the service checks the
// ticket is theirs.
var userMethods = map[string]struct{}{
	supportRPCPrefix + "ListSupportCategories": {},
	supportRPCPrefix + "CreateTicket":          {},
	supportRPCPrefix + "ListMyTickets":         {},
	supportRPCPrefix + "GetMyTicket":           {},
	supportRPCPrefix + "AddTicketMessage":      {},
	supportRPCPrefix + "CloseMyTicket":         {},
	supportRPCPrefix + "GetAttachmentURL":      {},
	supportRPCPrefix + "ListHelpSections":      {},
	supportRPCPrefix + "ListHelpArticles":      {},
	supportRPCPrefix + "GetHelpArticle":        {},
	supportRPCPrefix + "RateHelpArticle":       {},
	supportRPCPrefix + "RateTicket":            {},
}

// internalMethods take only the internal service token (identity-service
// gathering a person's data export); nothing else may call them.
var internalMethods = map[string]struct{}{
	supportRPCPrefix + "ExportPersonalData": {},
}

// staffMethods need the permission from staff-service before they run.
var staffMethods = map[string]string{
	supportRPCPrefix + "ListSupportQueue":           support.PermissionRead,
	supportRPCPrefix + "ListSafetyQueue":            support.PermissionSafety,
	supportRPCPrefix + "GetTicketForStaff":          support.PermissionRead,
	supportRPCPrefix + "ClaimTicket":                support.PermissionReply,
	supportRPCPrefix + "AssignTicket":               support.PermissionManage,
	supportRPCPrefix + "ReplyToTicket":              support.PermissionReply,
	supportRPCPrefix + "SetTicketStatus":            support.PermissionReply,
	supportRPCPrefix + "SetTicketPriority":          support.PermissionReply,
	supportRPCPrefix + "RequestTicketAction":        "", // by the action's kind
	supportRPCPrefix + "ListPendingTicketActions":   support.PermissionApprove,
	supportRPCPrefix + "ApproveTicketAction":        support.PermissionApprove,
	supportRPCPrefix + "RejectTicketAction":         support.PermissionApprove,
	supportRPCPrefix + "AdminListSupportCategories": support.PermissionConfigure,
	supportRPCPrefix + "UpsertSupportCategory":      support.PermissionConfigure,
	supportRPCPrefix + "AdminListHelpSections":      support.PermissionConfigure,
	supportRPCPrefix + "UpsertHelpSection":          support.PermissionConfigure,
	supportRPCPrefix + "AdminListHelpArticles":      support.PermissionConfigure,
	supportRPCPrefix + "UpsertHelpArticle":          support.PermissionConfigure,
	supportRPCPrefix + "ListMacros":                 support.PermissionReply,
	supportRPCPrefix + "AdminListMacros":            support.PermissionConfigure,
	supportRPCPrefix + "UpsertMacro":                support.PermissionConfigure,
	supportRPCPrefix + "GetSupportStats":            support.PermissionRead,
}

// StaffAuthorizer asks staff-service whether a staff member may use a
// permission, and records how an allowed call ended.
type StaffAuthorizer interface {
	Authorize(ctx context.Context, identityID, permission, method, targetID string) (allowed bool, staffID string, auditEntryID string, err error)
	Complete(ctx context.Context, auditEntryID string, code codes.Code)
}

func staffPermissionFor(method string, request any) string {
	if method == supportRPCPrefix+"RequestTicketAction" {
		r, ok := request.(*supportv1.RequestTicketActionRequest)
		if !ok {
			return ""
		}

		kind := actionKindFromProto(r.GetKind())
		if kind == "" {
			return ""
		}

		return support.PermissionForAction(kind)
	}

	return staffMethods[method]
}

// NewAuthorizationUnaryInterceptor fails closed: a method in no table is
// denied, and the internal token reaches internalMethods only.
func NewAuthorizationUnaryInterceptor(staff StaffAuthorizer) googlegrpc.UnaryServerInterceptor {
	if staff == nil {
		panic("staff authorizer is required")
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

		if info.FullMethod == healthv1.Health_Check_FullMethodName {
			return handler(ctx, request)
		}

		principal, ok := authenticatedPrincipalFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "valid access token is required")
		}

		_, internal := internalMethods[info.FullMethod]

		if principal.IdentityID == internalServicePrincipalID {
			if internal {
				return handler(ctx, request)
			}

			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}

		if internal {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}

		if _, user := userMethods[info.FullMethod]; user {
			return handler(ctx, request)
		}

		if _, staffMethod := staffMethods[info.FullMethod]; !staffMethod {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}

		permission := staffPermissionFor(info.FullMethod, request)
		if permission == "" {
			return nil, status.Error(codes.InvalidArgument, "kind is a known action")
		}

		allowed, staffID, auditEntryID, err := staff.Authorize(ctx, principal.IdentityID, permission, info.FullMethod, staffTargetOf(request))
		if err != nil {
			return nil, status.Error(codes.Unavailable, "permission could not be verified")
		}

		if !allowed {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}

		response, handlerErr := handler(
			contextWithStaff(ctx, support.Staff{StaffID: staffID, IdentityID: principal.IdentityID}),
			request,
		)
		staff.Complete(ctx, auditEntryID, status.Code(handlerErr))

		return response, handlerErr
	}
}

// staffTargetOf returns the id an admin request is about, for the audit log.
func staffTargetOf(request any) string {
	switch r := request.(type) {
	case interface{ GetTicketId() string }:
		return r.GetTicketId()
	case interface{ GetActionId() string }:
		return r.GetActionId()
	case *supportv1.UpsertSupportCategoryRequest:
		return r.GetCategory().GetKey()
	case *supportv1.UpsertHelpSectionRequest:
		return r.GetSection().GetKey()
	case *supportv1.UpsertHelpArticleRequest:
		return r.GetArticle().GetKey()
	case *supportv1.UpsertMacroRequest:
		return r.GetMacro().GetKey()
	default:
		return ""
	}
}

type staffContextKey struct{}

func contextWithStaff(ctx context.Context, staff support.Staff) context.Context {
	return context.WithValue(ctx, staffContextKey{}, staff)
}

func staffFromContext(ctx context.Context) (support.Staff, bool) {
	staff, ok := ctx.Value(staffContextKey{}).(support.Staff)

	return staff, ok && staff.StaffID != "" && staff.IdentityID != ""
}
