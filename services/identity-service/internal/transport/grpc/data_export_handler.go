package grpc

import (
	"context"
	"errors"
	"log/slog"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/dataexport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DataExportHandler serves /v1/me/data-exports for the signed-in person.
type DataExportHandler struct {
	identityv1.UnimplementedDataExportServiceServer

	service *dataexport.Service
	logger  *slog.Logger
}

func NewDataExportHandler(service *dataexport.Service, logger *slog.Logger) *DataExportHandler {
	if service == nil || logger == nil {
		panic("data export handler dependencies are required")
	}

	return &DataExportHandler{service: service, logger: logger}
}

func (h *DataExportHandler) RequestDataExport(ctx context.Context, _ *identityv1.RequestDataExportRequest) (*identityv1.DataExportResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated identity is required")
	}

	created, err := h.service.Request(ctx, principal.IdentityID)
	if err != nil {
		return nil, h.mapError(err)
	}

	return &identityv1.DataExportResponse{Export: toProtoDataExport(created)}, nil
}

func (h *DataExportHandler) ListDataExports(ctx context.Context, _ *identityv1.ListDataExportsRequest) (*identityv1.ListDataExportsResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated identity is required")
	}

	exports, next, err := h.service.List(ctx, principal.IdentityID)
	if err != nil {
		return nil, h.mapError(err)
	}

	response := &identityv1.ListDataExportsResponse{}
	if !next.IsZero() {
		response.NextRequestAt = timestamppb.New(next)
	}

	for _, e := range exports {
		response.Exports = append(response.Exports, toProtoDataExport(e))
	}

	return response, nil
}

func (h *DataExportHandler) GetDataExportDownload(ctx context.Context, request *identityv1.GetDataExportDownloadRequest) (*identityv1.DataExportDownloadResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated identity is required")
	}

	url, expires, err := h.service.Download(ctx, principal.IdentityID, request.GetExportId())
	if err != nil {
		return nil, h.mapError(err)
	}

	return &identityv1.DataExportDownloadResponse{Url: url, UrlExpiresAt: timestamppb.New(expires)}, nil
}

func toProtoDataExport(e dataexport.Export) *identityv1.DataExport {
	out := &identityv1.DataExport{
		Id:          e.ID,
		RequestedAt: timestamppb.New(e.RequestedAt),
		SizeBytes:   e.SizeBytes,
	}

	switch e.Status {
	case dataexport.StatusPending:
		out.Status = identityv1.DataExportStatus_DATA_EXPORT_STATUS_PENDING
	case dataexport.StatusReady:
		out.Status = identityv1.DataExportStatus_DATA_EXPORT_STATUS_READY
	case dataexport.StatusFailed:
		out.Status = identityv1.DataExportStatus_DATA_EXPORT_STATUS_FAILED
	case dataexport.StatusExpired:
		out.Status = identityv1.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED
	}

	if e.ReadyAt != nil {
		out.ReadyAt = timestamppb.New(*e.ReadyAt)
	}

	if e.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*e.ExpiresAt)
	}

	return out
}

func (h *DataExportHandler) mapError(err error) error {
	switch {
	case errors.Is(err, dataexport.ErrTooSoon):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, dataexport.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, dataexport.ErrNotReady):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		h.logger.Error("data export request failed", "error", err)

		return status.Error(codes.Internal, "failed to process the data export")
	}
}
