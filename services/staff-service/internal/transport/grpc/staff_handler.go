package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	staffv1 "github.com/7akoom/ride-platform/gen/go/ride/staff/v1"
	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StaffHandler struct {
	staffv1.UnimplementedStaffServiceServer

	service *staff.Service
	logger  *slog.Logger
}

func NewStaffHandler(service *staff.Service, logger *slog.Logger) *StaffHandler {
	if service == nil {
		panic("staff service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &StaffHandler{service: service, logger: logger}
}

func (h *StaffHandler) GetMyStaffProfile(
	ctx context.Context,
	_ *staffv1.GetMyStaffProfileRequest,
) (*staffv1.GetMyStaffProfileResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "valid access token is required")
	}

	member, permissions, err := h.service.MyProfile(ctx, principal.IdentityID)
	if err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.GetMyStaffProfileResponse{
		StaffMember: toProtoMember(member),
		Permissions: permissions,
	}, nil
}

func (h *StaffHandler) AcceptStaffInvite(
	ctx context.Context,
	_ *staffv1.AcceptStaffInviteRequest,
) (*staffv1.AcceptStaffInviteResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok || principal.IdentityID == internalServicePrincipalID {
		return nil, status.Error(codes.PermissionDenied, "a staff member's own access token is required")
	}

	accessToken, err := bearerTokenFromIncomingContext(ctx)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "valid access token is required")
	}

	member, permissions, err := h.service.AcceptInvite(ctx, principal.IdentityID, accessToken)
	if err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.AcceptStaffInviteResponse{
		StaffMember: toProtoMember(member),
		Permissions: permissions,
	}, nil
}

func (h *StaffHandler) ListStaffMembers(
	ctx context.Context,
	request *staffv1.ListStaffMembersRequest,
) (*staffv1.ListStaffMembersResponse, error) {
	statusFilter, err := fromProtoStatus(request.GetStatus())
	if err != nil {
		return nil, h.mapError(err)
	}

	page, err := h.service.ListMembers(ctx, staff.MemberListQuery{
		Status:    statusFilter,
		PageSize:  int(request.GetPageSize()),
		PageToken: request.GetPageToken(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	response := &staffv1.ListStaffMembersResponse{NextPageToken: page.NextPageToken}
	for _, member := range page.Members {
		response.StaffMembers = append(response.StaffMembers, toProtoMember(member))
	}

	return response, nil
}

func (h *StaffHandler) GetStaffMember(
	ctx context.Context,
	request *staffv1.GetStaffMemberRequest,
) (*staffv1.StaffMemberResponse, error) {
	member, err := h.service.GetMember(ctx, request.GetStaffId())

	return h.memberResponse(member, err)
}

func (h *StaffHandler) InviteStaffMember(
	ctx context.Context,
	request *staffv1.InviteStaffMemberRequest,
) (*staffv1.StaffMemberResponse, error) {
	member, err := h.service.Invite(ctx, actorFromContext(ctx), staff.InviteInput{
		Email:       request.GetEmail(),
		DisplayName: request.GetDisplayName(),
		RoleIDs:     request.GetRoleIds(),
	})

	return h.memberResponse(member, err)
}

func (h *StaffHandler) SetStaffRoles(
	ctx context.Context,
	request *staffv1.SetStaffRolesRequest,
) (*staffv1.StaffMemberResponse, error) {
	member, err := h.service.SetRoles(ctx, actorFromContext(ctx), request.GetStaffId(), request.GetRoleIds())

	return h.memberResponse(member, err)
}

func (h *StaffHandler) SuspendStaffMember(
	ctx context.Context,
	request *staffv1.SuspendStaffMemberRequest,
) (*staffv1.StaffMemberResponse, error) {
	member, err := h.service.Suspend(ctx, actorFromContext(ctx), request.GetStaffId())

	return h.memberResponse(member, err)
}

func (h *StaffHandler) ReactivateStaffMember(
	ctx context.Context,
	request *staffv1.ReactivateStaffMemberRequest,
) (*staffv1.StaffMemberResponse, error) {
	member, err := h.service.Reactivate(ctx, actorFromContext(ctx), request.GetStaffId())

	return h.memberResponse(member, err)
}

func (h *StaffHandler) RevokeStaffInvite(
	ctx context.Context,
	request *staffv1.RevokeStaffInviteRequest,
) (*staffv1.StaffMemberResponse, error) {
	member, err := h.service.RevokeInvite(ctx, actorFromContext(ctx), request.GetStaffId())

	return h.memberResponse(member, err)
}

func (h *StaffHandler) ListRoles(
	ctx context.Context,
	_ *staffv1.ListRolesRequest,
) (*staffv1.ListRolesResponse, error) {
	roles, err := h.service.ListRoles(ctx)
	if err != nil {
		return nil, h.mapError(err)
	}

	response := &staffv1.ListRolesResponse{}
	for _, role := range roles {
		response.Roles = append(response.Roles, toProtoRole(role))
	}

	return response, nil
}

func (h *StaffHandler) CreateRole(
	ctx context.Context,
	request *staffv1.CreateRoleRequest,
) (*staffv1.RoleResponse, error) {
	role, err := h.service.CreateRole(ctx, actorFromContext(ctx), staff.RoleInput{
		Name:        request.GetName(),
		Description: request.GetDescription(),
		Permissions: request.GetPermissions(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.RoleResponse{Role: toProtoRole(role)}, nil
}

func (h *StaffHandler) UpdateRole(
	ctx context.Context,
	request *staffv1.UpdateRoleRequest,
) (*staffv1.RoleResponse, error) {
	role, err := h.service.UpdateRole(ctx, actorFromContext(ctx), staff.RoleInput{
		ID:          request.GetRoleId(),
		Name:        request.GetName(),
		Description: request.GetDescription(),
		Permissions: request.GetPermissions(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.RoleResponse{Role: toProtoRole(role)}, nil
}

func (h *StaffHandler) DeleteRole(
	ctx context.Context,
	request *staffv1.DeleteRoleRequest,
) (*staffv1.DeleteRoleResponse, error) {
	if err := h.service.DeleteRole(ctx, actorFromContext(ctx), request.GetRoleId()); err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.DeleteRoleResponse{}, nil
}

func (h *StaffHandler) ListPermissions(
	_ context.Context,
	_ *staffv1.ListPermissionsRequest,
) (*staffv1.ListPermissionsResponse, error) {
	response := &staffv1.ListPermissionsResponse{}
	for _, permission := range h.service.ListPermissions() {
		response.Permissions = append(response.Permissions, &staffv1.Permission{
			Key:         permission.Key,
			Description: permission.Description,
		})
	}

	return response, nil
}

func (h *StaffHandler) ListAuditEntries(
	ctx context.Context,
	request *staffv1.ListAuditEntriesRequest,
) (*staffv1.ListAuditEntriesResponse, error) {
	query := staff.AuditListQuery{
		ActorStaffID: request.GetActorStaffId(),
		Permission:   request.GetPermission(),
		TargetID:     request.GetTargetId(),
		PageSize:     int(request.GetPageSize()),
		PageToken:    request.GetPageToken(),
	}

	if request.GetOccurredAfter() != nil {
		after := request.GetOccurredAfter().AsTime()
		query.OccurredAfter = &after
	}

	if request.GetOccurredBefore() != nil {
		before := request.GetOccurredBefore().AsTime()
		query.OccurredBefore = &before
	}

	page, err := h.service.ListAudit(ctx, query)
	if err != nil {
		return nil, h.mapError(err)
	}

	response := &staffv1.ListAuditEntriesResponse{NextPageToken: page.NextPageToken}
	for _, entry := range page.Entries {
		response.Entries = append(response.Entries, toProtoAuditEntry(entry))
	}

	return response, nil
}

func (h *StaffHandler) AuthorizeStaffAction(
	ctx context.Context,
	request *staffv1.AuthorizeStaffActionRequest,
) (*staffv1.AuthorizeStaffActionResponse, error) {
	decision, err := h.service.Authorize(ctx, staff.AuthorizeInput{
		IdentityID: request.GetIdentityId(),
		Permission: request.GetPermission(),
		Method:     request.GetMethod(),
		TargetID:   request.GetTargetId(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.AuthorizeStaffActionResponse{
		Allowed:      decision.Allowed,
		AuditEntryId: decision.AuditEntryID,
		StaffId:      decision.Actor.Member.ID,
	}, nil
}

func (h *StaffHandler) CompleteStaffAction(
	ctx context.Context,
	request *staffv1.CompleteStaffActionRequest,
) (*staffv1.CompleteStaffActionResponse, error) {
	if err := h.service.CompleteAction(ctx, request.GetAuditEntryId(), request.GetOutcomeCode()); err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.CompleteStaffActionResponse{}, nil
}

func (h *StaffHandler) memberResponse(member staff.Member, err error) (*staffv1.StaffMemberResponse, error) {
	if err != nil {
		return nil, h.mapError(err)
	}

	return &staffv1.StaffMemberResponse{StaffMember: toProtoMember(member)}, nil
}

func (h *StaffHandler) mapError(err error) error {
	switch {
	case errors.Is(err, staff.ErrInvalidEmail),
		errors.Is(err, staff.ErrDisplayNameRequired),
		errors.Is(err, staff.ErrDisplayNameTooLong),
		errors.Is(err, staff.ErrRoleNameRequired),
		errors.Is(err, staff.ErrRoleNameTooLong),
		errors.Is(err, staff.ErrDescriptionTooLong),
		errors.Is(err, staff.ErrUnknownPermission),
		errors.Is(err, staff.ErrRolesRequired),
		errors.Is(err, staff.ErrInvalidID),
		errors.Is(err, staff.ErrInvalidPageSize),
		errors.Is(err, staff.ErrInvalidPageToken),
		errors.Is(err, staff.ErrInvalidStatus),
		errors.Is(err, staff.ErrInvalidAuditRequest),
		errors.Is(err, staff.ErrInvalidOutcomeCode),
		errors.Is(err, staff.ErrInvalidTimeRange):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, staff.ErrNotStaff),
		errors.Is(err, staff.ErrMemberNotFound),
		errors.Is(err, staff.ErrRoleNotFound),
		errors.Is(err, staff.ErrAuditEntryNotFound),
		errors.Is(err, staff.ErrNoInvitation):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, staff.ErrEmailAlreadyInvited),
		errors.Is(err, staff.ErrIdentityAlreadyStaff),
		errors.Is(err, staff.ErrRoleNameTaken):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, staff.ErrInvalidTransition),
		errors.Is(err, staff.ErrLastOwner),
		errors.Is(err, staff.ErrRoleInUse),
		errors.Is(err, staff.ErrSystemRole),
		errors.Is(err, staff.ErrIdentityNotActive):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, staff.ErrIdentityRejected):
		return status.Error(codes.Unauthenticated, err.Error())

	case errors.Is(err, staff.ErrSelfAction),
		errors.Is(err, staff.ErrPermissionEscalation),
		errors.Is(err, staff.ErrOwnerRoleRequired),
		errors.Is(err, staff.ErrPermissionDenied),
		errors.Is(err, staff.ErrStaffNotAuthenticated):
		return status.Error(codes.PermissionDenied, err.Error())
	}

	h.logger.Error("staff request failed", "error", err)

	return status.Error(codes.Internal, "internal error")
}

func fromProtoStatus(value staffv1.StaffStatus) (staff.Status, error) {
	switch value {
	case staffv1.StaffStatus_STAFF_STATUS_UNSPECIFIED:
		return "", nil
	case staffv1.StaffStatus_STAFF_STATUS_INVITED:
		return staff.StatusInvited, nil
	case staffv1.StaffStatus_STAFF_STATUS_ACTIVE:
		return staff.StatusActive, nil
	case staffv1.StaffStatus_STAFF_STATUS_SUSPENDED:
		return staff.StatusSuspended, nil
	case staffv1.StaffStatus_STAFF_STATUS_REVOKED:
		return staff.StatusRevoked, nil
	default:
		return "", staff.ErrInvalidStatus
	}
}

func toProtoStatus(value staff.Status) staffv1.StaffStatus {
	switch value {
	case staff.StatusInvited:
		return staffv1.StaffStatus_STAFF_STATUS_INVITED
	case staff.StatusActive:
		return staffv1.StaffStatus_STAFF_STATUS_ACTIVE
	case staff.StatusSuspended:
		return staffv1.StaffStatus_STAFF_STATUS_SUSPENDED
	case staff.StatusRevoked:
		return staffv1.StaffStatus_STAFF_STATUS_REVOKED
	default:
		return staffv1.StaffStatus_STAFF_STATUS_UNSPECIFIED
	}
}

func toProtoMember(member staff.Member) *staffv1.StaffMember {
	out := &staffv1.StaffMember{
		Id:               member.ID,
		IdentityId:       member.IdentityID,
		Email:            member.Email,
		DisplayName:      member.DisplayName,
		Status:           toProtoStatus(member.Status),
		InvitedByStaffId: member.InvitedByStaffID,
		InvitedAt:        timestamp(member.InvitedAt),
		ActivatedAt:      optionalTimestamp(member.ActivatedAt),
		CreatedAt:        timestamp(member.CreatedAt),
		UpdatedAt:        timestamp(member.UpdatedAt),
	}

	for _, role := range member.Roles {
		out.Roles = append(out.Roles, toProtoRole(role))
	}

	return out
}

func toProtoRole(role staff.Role) *staffv1.Role {
	return &staffv1.Role{
		Id:          role.ID,
		Key:         role.Key,
		Name:        role.Name,
		Description: role.Description,
		System:      role.System,
		Permissions: role.Grants(),
		CreatedAt:   timestamp(role.CreatedAt),
		UpdatedAt:   timestamp(role.UpdatedAt),
	}
}

func toProtoAuditEntry(entry staff.AuditEntry) *staffv1.AuditEntry {
	out := &staffv1.AuditEntry{
		Id:              entry.ID,
		OccurredAt:      timestamp(entry.OccurredAt),
		ActorStaffId:    entry.ActorStaffID,
		ActorIdentityId: entry.ActorIdentityID,
		Permission:      entry.Permission,
		Method:          entry.Method,
		TargetId:        entry.TargetID,
		OutcomeCode:     entry.OutcomeCode,
		CompletedAt:     optionalTimestamp(entry.CompletedAt),
	}

	switch entry.Decision {
	case staff.DecisionAllowed:
		out.Decision = staffv1.AuditDecision_AUDIT_DECISION_ALLOWED
	case staff.DecisionDenied:
		out.Decision = staffv1.AuditDecision_AUDIT_DECISION_DENIED
	}

	switch entry.Outcome {
	case staff.OutcomePending:
		out.Outcome = staffv1.AuditOutcome_AUDIT_OUTCOME_PENDING
	case staff.OutcomeSucceeded:
		out.Outcome = staffv1.AuditOutcome_AUDIT_OUTCOME_SUCCEEDED
	case staff.OutcomeFailed:
		out.Outcome = staffv1.AuditOutcome_AUDIT_OUTCOME_FAILED
	}

	return out
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}

	return timestamppb.New(value)
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}

	return timestamppb.New(*value)
}
