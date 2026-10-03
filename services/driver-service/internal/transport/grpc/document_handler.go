package grpc

import (
	"context"
	"errors"
	"strings"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

func (h *DriverHandler) ListDriverDocumentTypes(
	ctx context.Context,
	_ *driverv1.ListDriverDocumentTypesRequest,
) (*driverv1.ListDriverDocumentTypesResponse, error) {
	return h.listTypes(ctx, false)
}

func (h *DriverHandler) AdminListDriverDocumentTypes(
	ctx context.Context,
	_ *driverv1.ListDriverDocumentTypesRequest,
) (*driverv1.ListDriverDocumentTypesResponse, error) {
	return h.listTypes(ctx, true)
}

func (h *DriverHandler) listTypes(ctx context.Context, includeInactive bool) (*driverv1.ListDriverDocumentTypesResponse, error) {
	types, err := h.documents.ListTypes(ctx, includeInactive)
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	response := &driverv1.ListDriverDocumentTypesResponse{}
	for _, t := range types {
		response.Types = append(response.Types, toProtoDocumentType(t))
	}

	return response, nil
}

func (h *DriverHandler) UpsertDriverDocumentType(
	ctx context.Context,
	request *driverv1.UpsertDriverDocumentTypeRequest,
) (*driverv1.DriverDocumentTypeResponse, error) {
	if request == nil || request.GetType() == nil {
		return nil, status.Error(codes.InvalidArgument, "type is required")
	}

	t := request.GetType()

	saved, err := h.documents.UpsertType(ctx, documents.Type{
		Code:           request.GetCode(),
		Scope:          documents.Scope(t.GetScope()),
		MediaPurpose:   documents.MediaPurpose(t.GetMediaPurpose()),
		NameEN:         t.GetNameEn(),
		NameAR:         t.GetNameAr(),
		NameKU:         t.GetNameKu(),
		Required:       t.GetRequired(),
		RequiresNumber: t.GetRequiresNumber(),
		RequiresExpiry: t.GetRequiresExpiry(),
		Active:         t.GetActive(),
		SortOrder:      int(t.GetSortOrder()),
	})
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	return &driverv1.DriverDocumentTypeResponse{Type: toProtoDocumentType(saved)}, nil
}

func (h *DriverHandler) SubmitDriverDocument(
	ctx context.Context,
	request *driverv1.SubmitDriverDocumentRequest,
) (*driverv1.DriverDocumentResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	created, err := h.documents.Submit(ctx, documents.SubmitInput{
		DriverID:  request.GetDriverId(),
		VehicleID: request.GetVehicleId(),
		TypeCode:  request.GetTypeCode(),
		MediaID:   request.GetMediaId(),
		Number:    request.GetDocumentNumber(),
		ExpiresOn: request.GetExpiresOn(),
	})
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	return &driverv1.DriverDocumentResponse{Document: toProtoDocument(created, h.documents.Today())}, nil
}

func (h *DriverHandler) ListDriverDocuments(
	ctx context.Context,
	request *driverv1.ListDriverDocumentsRequest,
) (*driverv1.ListDriverDocumentsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	overview, err := h.documents.Overview(ctx, request.GetDriverId(), request.GetIncludeHistory(), request.GetVehicleId())
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	response := &driverv1.ListDriverDocumentsResponse{Compliant: overview.Compliant}

	if overview.Vehicle != nil {
		car, err := h.vehicles.Get(ctx, overview.Vehicle.ID)
		if err != nil {
			return nil, h.mapVehicleError(err)
		}

		response.Vehicle = toProtoVehicle(car)
	}

	for _, req := range overview.Requirements {
		response.Requirements = append(response.Requirements, &driverv1.DocumentRequirement{
			Type:      toProtoDocumentType(req.Type),
			State:     toProtoRequirementState(req.State),
			Approved:  toProtoDocumentOrNil(req.Approved, overview.Today),
			Pending:   toProtoDocumentOrNil(req.Pending, overview.Today),
			Rejected:  toProtoDocumentOrNil(req.Rejected, overview.Today),
			VehicleId: req.VehicleID,
		})
	}

	for _, d := range overview.History {
		response.History = append(response.History, toProtoDocument(d, overview.Today))
	}

	return response, nil
}

func (h *DriverHandler) ListPendingDriverDocuments(
	ctx context.Context,
	request *driverv1.ListPendingDriverDocumentsRequest,
) (*driverv1.ListPendingDriverDocumentsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	page, err := h.documents.Pending(ctx, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	today := h.documents.Today()
	response := &driverv1.ListPendingDriverDocumentsResponse{NextPageToken: page.NextPageToken}

	for _, item := range page.Items {
		response.Documents = append(response.Documents, &driverv1.PendingDriverDocument{
			Document:          toProtoDocument(item.Document, today),
			DriverDisplayName: item.DriverDisplayName,
			DriverStatus:      toProtoDriverStatus(driver.Status(item.DriverStatus)),
		})
	}

	return response, nil
}

func (h *DriverHandler) ApproveDriverDocument(
	ctx context.Context,
	request *driverv1.ApproveDriverDocumentRequest,
) (*driverv1.DriverDocumentResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	approved, err := h.documents.Approve(ctx, documents.ApproveInput{
		DocumentID: request.GetDocumentId(),
		Number:     request.GetDocumentNumber(),
		ExpiresOn:  request.GetExpiresOn(),
		ReviewedBy: reviewerOf(ctx),
	})
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	return &driverv1.DriverDocumentResponse{Document: toProtoDocument(approved, h.documents.Today())}, nil
}

func (h *DriverHandler) RejectDriverDocument(
	ctx context.Context,
	request *driverv1.RejectDriverDocumentRequest,
) (*driverv1.DriverDocumentResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	rejected, err := h.documents.Reject(ctx, documents.RejectInput{
		DocumentID: request.GetDocumentId(),
		Reason:     request.GetReason(),
		ReviewedBy: reviewerOf(ctx),
	})
	if err != nil {
		return nil, h.mapDocumentError(err)
	}

	return &driverv1.DriverDocumentResponse{Document: toProtoDocument(rejected, h.documents.Today())}, nil
}

// reviewerOf is the staff member acting; empty for the internal token.
func reviewerOf(ctx context.Context) string {
	if !isStaffCall(ctx) {
		return ""
	}

	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return ""
	}

	return principal.IdentityID
}

func (h *DriverHandler) mapDocumentError(err error) error {
	switch {
	case errors.Is(err, documents.ErrDriverNotFound), errors.Is(err, driver.ErrDriverNotFound):
		return status.Error(codes.NotFound, "driver not found")
	case errors.Is(err, documents.ErrTypeNotFound):
		return status.Error(codes.NotFound, "document type not found")
	case errors.Is(err, documents.ErrDocumentNotFound):
		return status.Error(codes.NotFound, "document not found")
	case errors.Is(err, documents.ErrVehicleNotFound):
		return status.Error(codes.NotFound, "vehicle not found")
	case errors.Is(err, documents.ErrNoActiveVehicle),
		errors.Is(err, documents.ErrVehicleRetired):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, documents.ErrMediaAlreadyUsed),
		errors.Is(err, documents.ErrNumberTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, documents.ErrMediaNotUsable),
		errors.Is(err, documents.ErrDocumentNotPending),
		errors.Is(err, documents.ErrDocumentNotReviewable):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, documents.ErrMediaUnavailable):
		h.logger.Error("media-service could not hold a document file", "error", err)

		return status.Error(codes.Unavailable, "the file could not be checked, try again")
	case errors.Is(err, documents.ErrInvalidTypeCode),
		errors.Is(err, documents.ErrInvalidType),
		errors.Is(err, documents.ErrMediaIDRequired),
		errors.Is(err, documents.ErrInvalidMediaID),
		errors.Is(err, documents.ErrNumberRequired),
		errors.Is(err, documents.ErrInvalidNumber),
		errors.Is(err, documents.ErrExpiryRequired),
		errors.Is(err, documents.ErrInvalidExpiryDate),
		errors.Is(err, documents.ErrExpiryNotInFuture),
		errors.Is(err, documents.ErrExpiryTooFar),
		errors.Is(err, documents.ErrReasonRequired),
		errors.Is(err, documents.ErrReasonTooLong),
		errors.Is(err, documents.ErrInvalidPageToken),
		errors.Is(err, documents.ErrInvalidPageSize):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		h.logger.Error("unclassified driver document failure", "error", err)

		return status.Error(codes.Internal, "failed to process driver document request")
	}
}

// documentsMessage names the missing documents for the app.
func documentsMessage(err error) string {
	_, missing, found := strings.Cut(err.Error(), driver.ErrDocumentsIncomplete.Error()+": ")
	if !found || missing == "" {
		return driver.ErrDocumentsIncomplete.Error()
	}

	return driver.ErrDocumentsIncomplete.Error() + ": " + missing
}

func toProtoDocumentType(t documents.Type) *driverv1.DriverDocumentType {
	return &driverv1.DriverDocumentType{
		Code:           t.Code,
		Scope:          string(t.Scope),
		MediaPurpose:   string(t.MediaPurpose),
		NameEn:         t.NameEN,
		NameAr:         t.NameAR,
		NameKu:         t.NameKU,
		Required:       t.Required,
		RequiresNumber: t.RequiresNumber,
		RequiresExpiry: t.RequiresExpiry,
		Active:         t.Active,
		SortOrder:      int32(t.SortOrder),
	}
}

func toProtoDocumentOrNil(d *documents.Document, today documents.Date) *driverv1.DriverDocument {
	if d == nil {
		return nil
	}

	return toProtoDocument(*d, today)
}

func toProtoDocument(d documents.Document, today documents.Date) *driverv1.DriverDocument {
	out := &driverv1.DriverDocument{
		Id:              d.ID,
		DriverId:        d.DriverID,
		TypeCode:        d.TypeCode,
		MediaId:         d.MediaID,
		DocumentNumber:  d.Number,
		ExpiresOn:       d.ExpiresOn.String(),
		Status:          toProtoDocumentStatus(d.Status),
		RejectionReason: d.RejectionReason,
		SubmittedAt:     timestamppb.New(d.CreatedAt),
		Expired:         d.ExpiredOn(today),
		VehicleId:       d.VehicleID,
	}

	if !d.ReviewedAt.IsZero() {
		out.ReviewedAt = timestamppb.New(d.ReviewedAt)
	}

	return out
}

func toProtoDocumentStatus(s documents.Status) driverv1.DriverDocumentStatus {
	switch s {
	case documents.StatusPending:
		return driverv1.DriverDocumentStatus_DRIVER_DOCUMENT_STATUS_PENDING
	case documents.StatusApproved:
		return driverv1.DriverDocumentStatus_DRIVER_DOCUMENT_STATUS_APPROVED
	case documents.StatusRejected:
		return driverv1.DriverDocumentStatus_DRIVER_DOCUMENT_STATUS_REJECTED
	case documents.StatusSuperseded:
		return driverv1.DriverDocumentStatus_DRIVER_DOCUMENT_STATUS_SUPERSEDED
	default:
		return driverv1.DriverDocumentStatus_DRIVER_DOCUMENT_STATUS_UNSPECIFIED
	}
}

func toProtoRequirementState(s documents.State) driverv1.DocumentRequirementState {
	switch s {
	case documents.StateMissing:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_MISSING
	case documents.StatePendingReview:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_PENDING_REVIEW
	case documents.StateRejected:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_REJECTED
	case documents.StateApproved:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_APPROVED
	case documents.StateExpiringSoon:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_EXPIRING_SOON
	case documents.StateExpired:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_EXPIRED
	default:
		return driverv1.DocumentRequirementState_DOCUMENT_REQUIREMENT_STATE_UNSPECIFIED
	}
}

func toProtoDriverStatus(s driver.Status) driverv1.DriverStatus {
	switch s {
	case driver.StatusPending:
		return driverv1.DriverStatus_DRIVER_STATUS_PENDING
	case driver.StatusActive:
		return driverv1.DriverStatus_DRIVER_STATUS_ACTIVE
	case driver.StatusRejected:
		return driverv1.DriverStatus_DRIVER_STATUS_REJECTED
	case driver.StatusSuspended:
		return driverv1.DriverStatus_DRIVER_STATUS_SUSPENDED
	default:
		return driverv1.DriverStatus_DRIVER_STATUS_UNSPECIFIED
	}
}
