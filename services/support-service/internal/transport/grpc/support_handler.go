package grpc

import (
	"context"
	"errors"
	"log/slog"

	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

type SupportHandler struct {
	supportv1.UnimplementedSupportServiceServer

	service *support.Service
	logger  *slog.Logger
}

func NewSupportHandler(service *support.Service, logger *slog.Logger) *SupportHandler {
	if service == nil {
		panic("support service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &SupportHandler{service: service, logger: logger}
}

func (h *SupportHandler) mapError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, support.ErrNotFound), errors.Is(err, support.ErrActionNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, support.ErrInvalidInput), errors.Is(err, support.ErrCategoryNotFound):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, support.ErrTooManyOpen):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, support.ErrOwnApproval), errors.Is(err, support.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, support.ErrNoProfile), errors.Is(err, support.ErrTripNotYours),
		errors.Is(err, support.ErrTripTooOld), errors.Is(err, support.ErrTripRequired),
		errors.Is(err, support.ErrTripNotCompleted), errors.Is(err, support.ErrTicketClosed),
		errors.Is(err, support.ErrAlreadyAssigned), errors.Is(err, support.ErrSafetyPriority),
		errors.Is(err, support.ErrActionNotAllowed), errors.Is(err, support.ErrNothingToWaive),
		errors.Is(err, support.ErrActionNotPending), errors.Is(err, support.ErrMediaNotUsable),
		errors.Is(err, support.ErrAttachmentInUse):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, support.ErrUpstreamUnavailable):
		h.logger.WarnContext(ctx, "support request could not reach a service", "error", err)

		return status.Error(codes.Unavailable, support.ErrUpstreamUnavailable.Error())
	default:
		h.logger.ErrorContext(ctx, "support request failed", "error", err)

		return status.Error(codes.Internal, "internal error")
	}
}

func caller(ctx context.Context) (support.Caller, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok || principal.IdentityID == internalServicePrincipalID {
		return support.Caller{}, status.Error(codes.Unauthenticated, "valid access token is required")
	}

	return support.Caller{IdentityID: principal.IdentityID}, nil
}

func staffCaller(ctx context.Context) (support.Staff, error) {
	staff, ok := staffFromContext(ctx)
	if !ok {
		return support.Staff{}, status.Error(codes.PermissionDenied, "permission denied")
	}

	return staff, nil
}

// --- people ---------------------------------------------------------------

func (h *SupportHandler) ListSupportCategories(
	ctx context.Context,
	request *supportv1.ListSupportCategoriesRequest,
) (*supportv1.ListSupportCategoriesResponse, error) {
	categories, err := h.service.ListCategories(ctx, audienceFromProto(request.GetAudience()))
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.ListSupportCategoriesResponse{Categories: categoriesToProto(categories)}, nil
}

func (h *SupportHandler) CreateTicket(
	ctx context.Context,
	request *supportv1.CreateTicketRequest,
) (*supportv1.CreateTicketResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	view, err := h.service.CreateTicket(ctx, who, support.CreateTicketInput{
		Audience:      audienceFromProto(request.GetAudience()),
		CategoryKey:   request.GetCategoryKey(),
		Subject:       request.GetSubject(),
		Body:          request.GetBody(),
		TripID:        request.GetTripId(),
		TransactionID: request.GetTransactionId(),
		AttachmentIDs: request.GetAttachmentMediaIds(),
	})
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.CreateTicketResponse{
		Ticket:   ticketToProto(view.Ticket, view.Role),
		Messages: messagesToProto(view.Messages, false),
		Existing: view.Existing,
	}, nil
}

func (h *SupportHandler) ListMyTickets(
	ctx context.Context,
	request *supportv1.ListMyTicketsRequest,
) (*supportv1.ListMyTicketsResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	tickets, next, err := h.service.ListMyTickets(ctx, who, support.MyTicketsQuery{
		Audience: audienceFromProto(request.GetAudience()),
		OpenOnly: request.GetOpenOnly(),
		Limit:    int(request.GetPageSize()),
		Cursor:   request.GetPageToken(),
	})
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	out := make([]*supportv1.Ticket, 0, len(tickets))

	for _, t := range tickets {
		role := support.RoleParticipant
		if t.RequesterIdentityID == who.IdentityID {
			role = support.RoleRequester
		}

		out = append(out, ticketToProto(t, role))
	}

	return &supportv1.ListMyTicketsResponse{Tickets: out, NextPageToken: next}, nil
}

func (h *SupportHandler) GetMyTicket(
	ctx context.Context,
	request *supportv1.GetMyTicketRequest,
) (*supportv1.TicketDetailResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	view, err := h.service.GetMyTicket(ctx, who, request.GetTicketId())
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return userDetail(view), nil
}

func (h *SupportHandler) AddTicketMessage(
	ctx context.Context,
	request *supportv1.AddTicketMessageRequest,
) (*supportv1.TicketDetailResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	view, err := h.service.AddTicketMessage(ctx, who, request.GetTicketId(), request.GetBody(), request.GetAttachmentMediaIds())
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return userDetail(view), nil
}

func (h *SupportHandler) CloseMyTicket(
	ctx context.Context,
	request *supportv1.CloseMyTicketRequest,
) (*supportv1.TicketDetailResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	view, err := h.service.CloseMyTicket(ctx, who, request.GetTicketId())
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return userDetail(view), nil
}

func (h *SupportHandler) GetAttachmentURL(
	ctx context.Context,
	request *supportv1.GetAttachmentURLRequest,
) (*supportv1.GetAttachmentURLResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	url, expiresAt, err := h.service.AttachmentURL(ctx, who, request.GetTicketId(), request.GetMediaId(),
		supportv1.SupportService_GetAttachmentURL_FullMethodName)
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.GetAttachmentURLResponse{Url: url, ExpiresAt: timestamppb.New(expiresAt)}, nil
}

// --- staff ----------------------------------------------------------------

func queueQuery(request *supportv1.ListSupportQueueRequest, safety bool) support.QueueQuery {
	return support.QueueQuery{
		Safety:          safety,
		Status:          statusFromProto(request.GetStatus()),
		CategoryKey:     request.GetCategoryKey(),
		Priority:        priorityFromProto(request.GetPriority()),
		AssignedStaffID: request.GetAssignedStaffId(),
		UnassignedOnly:  request.GetUnassignedOnly(),
		Audience:        audienceFromProto(request.GetAudience()),
		Limit:           int(request.GetPageSize()),
		Cursor:          request.GetPageToken(),
	}
}

func (h *SupportHandler) listQueue(ctx context.Context, request *supportv1.ListSupportQueueRequest, safety bool) (*supportv1.ListSupportQueueResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	tickets, next, err := h.service.ListQueue(ctx, queueQuery(request, safety))
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	out := make([]*supportv1.StaffTicket, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, staffTicketToProto(t))
	}

	return &supportv1.ListSupportQueueResponse{Tickets: out, NextPageToken: next}, nil
}

func (h *SupportHandler) ListSupportQueue(ctx context.Context, request *supportv1.ListSupportQueueRequest) (*supportv1.ListSupportQueueResponse, error) {
	return h.listQueue(ctx, request, false)
}

func (h *SupportHandler) ListSafetyQueue(ctx context.Context, request *supportv1.ListSupportQueueRequest) (*supportv1.ListSupportQueueResponse, error) {
	return h.listQueue(ctx, request, true)
}

func (h *SupportHandler) staffTicketCall(
	ctx context.Context,
	call func(staff support.Staff) (support.StaffTicketView, error),
) (*supportv1.StaffTicketDetailResponse, error) {
	staff, err := staffCaller(ctx)
	if err != nil {
		return nil, err
	}

	view, err := call(staff)
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return staffDetail(view), nil
}

func (h *SupportHandler) GetTicketForStaff(ctx context.Context, request *supportv1.GetTicketForStaffRequest) (*supportv1.StaffTicketDetailResponse, error) {
	return h.staffTicketCall(ctx, func(staff support.Staff) (support.StaffTicketView, error) {
		return h.service.GetTicketForStaff(ctx, staff, request.GetTicketId(), supportv1.SupportService_GetTicketForStaff_FullMethodName)
	})
}

func (h *SupportHandler) ClaimTicket(ctx context.Context, request *supportv1.ClaimTicketRequest) (*supportv1.StaffTicketDetailResponse, error) {
	return h.staffTicketCall(ctx, func(staff support.Staff) (support.StaffTicketView, error) {
		return h.service.ClaimTicket(ctx, staff, request.GetTicketId(), supportv1.SupportService_ClaimTicket_FullMethodName)
	})
}

func (h *SupportHandler) AssignTicket(ctx context.Context, request *supportv1.AssignTicketRequest) (*supportv1.StaffTicketDetailResponse, error) {
	return h.staffTicketCall(ctx, func(staff support.Staff) (support.StaffTicketView, error) {
		return h.service.AssignTicket(ctx, staff, request.GetTicketId(), request.GetStaffId(), supportv1.SupportService_AssignTicket_FullMethodName)
	})
}

func (h *SupportHandler) ReplyToTicket(ctx context.Context, request *supportv1.ReplyToTicketRequest) (*supportv1.StaffTicketDetailResponse, error) {
	return h.staffTicketCall(ctx, func(staff support.Staff) (support.StaffTicketView, error) {
		setStatus := support.Status("")
		if request.GetSetStatus() != supportv1.TicketStatus_TICKET_STATUS_UNSPECIFIED {
			if setStatus = statusFromProto(request.GetSetStatus()); setStatus == "" {
				return support.StaffTicketView{}, support.ErrInvalidInput
			}
		}

		return h.service.Reply(ctx, staff, support.ReplyInput{
			TicketID:      request.GetTicketId(),
			Body:          request.GetBody(),
			Internal:      request.GetInternal(),
			AttachmentIDs: request.GetAttachmentMediaIds(),
			SetStatus:     setStatus,
		}, supportv1.SupportService_ReplyToTicket_FullMethodName)
	})
}

func (h *SupportHandler) SetTicketStatus(ctx context.Context, request *supportv1.SetTicketStatusRequest) (*supportv1.StaffTicketDetailResponse, error) {
	return h.staffTicketCall(ctx, func(staff support.Staff) (support.StaffTicketView, error) {
		return h.service.SetStatus(ctx, staff, request.GetTicketId(), statusFromProto(request.GetStatus()),
			supportv1.SupportService_SetTicketStatus_FullMethodName)
	})
}

func (h *SupportHandler) SetTicketPriority(ctx context.Context, request *supportv1.SetTicketPriorityRequest) (*supportv1.StaffTicketDetailResponse, error) {
	return h.staffTicketCall(ctx, func(staff support.Staff) (support.StaffTicketView, error) {
		return h.service.SetPriority(ctx, staff, request.GetTicketId(), priorityFromProto(request.GetPriority()),
			supportv1.SupportService_SetTicketPriority_FullMethodName)
	})
}

func (h *SupportHandler) RequestTicketAction(ctx context.Context, request *supportv1.RequestTicketActionRequest) (*supportv1.TicketActionResponse, error) {
	staff, err := staffCaller(ctx)
	if err != nil {
		return nil, err
	}

	input := support.ActionInput{
		TicketID:     request.GetTicketId(),
		Kind:         actionKindFromProto(request.GetKind()),
		Amount:       request.GetAmount(),
		DriverAmount: request.GetDriverAmount(),
		Target:       actionTargetFromProto(request.GetTarget()),
		Reason:       request.GetReason(),
	}

	if request.GetSuspendUntil() != nil {
		until := request.GetSuspendUntil().AsTime()
		input.SuspendUntil = &until
	}

	action, err := h.service.RequestAction(ctx, staff, input, supportv1.SupportService_RequestTicketAction_FullMethodName)
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.TicketActionResponse{Action: actionToProto(action)}, nil
}

func (h *SupportHandler) ListPendingTicketActions(
	ctx context.Context,
	request *supportv1.ListPendingTicketActionsRequest,
) (*supportv1.ListPendingTicketActionsResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	actions, next, err := h.service.ListPendingActions(ctx, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.ListPendingTicketActionsResponse{Actions: actionsToProto(actions), NextPageToken: next}, nil
}

func (h *SupportHandler) ApproveTicketAction(ctx context.Context, request *supportv1.ApproveTicketActionRequest) (*supportv1.TicketActionResponse, error) {
	staff, err := staffCaller(ctx)
	if err != nil {
		return nil, err
	}

	action, err := h.service.ApproveAction(ctx, staff, request.GetActionId(), request.GetReason())
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.TicketActionResponse{Action: actionToProto(action)}, nil
}

func (h *SupportHandler) RejectTicketAction(ctx context.Context, request *supportv1.RejectTicketActionRequest) (*supportv1.TicketActionResponse, error) {
	staff, err := staffCaller(ctx)
	if err != nil {
		return nil, err
	}

	action, err := h.service.RejectAction(ctx, staff, request.GetActionId(), request.GetReason())
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.TicketActionResponse{Action: actionToProto(action)}, nil
}

func (h *SupportHandler) AdminListSupportCategories(
	ctx context.Context,
	_ *supportv1.AdminListSupportCategoriesRequest,
) (*supportv1.ListSupportCategoriesResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	categories, err := h.service.AdminListCategories(ctx)
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.ListSupportCategoriesResponse{Categories: categoriesToProto(categories)}, nil
}

func (h *SupportHandler) UpsertSupportCategory(
	ctx context.Context,
	request *supportv1.UpsertSupportCategoryRequest,
) (*supportv1.SupportCategoryResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	c := request.GetCategory()

	saved, err := h.service.UpsertCategory(ctx, support.Category{
		Key:             c.GetKey(),
		Audience:        categoryAudienceFromProto(c.GetAudience()),
		NameEn:          c.GetNameEn(),
		NameAr:          c.GetNameAr(),
		NameKu:          c.GetNameKu(),
		DefaultPriority: priorityFromProto(c.GetDefaultPriority()),
		RequiresTrip:    c.GetRequiresTrip(),
		Safety:          c.GetSafety(),
		LostItem:        c.GetLostItem(),
		Active:          c.GetActive(),
		SortOrder:       int(c.GetSortOrder()),
	})
	if err != nil {
		return nil, h.mapError(ctx, err)
	}

	return &supportv1.SupportCategoryResponse{Category: categoryToProto(saved)}, nil
}
