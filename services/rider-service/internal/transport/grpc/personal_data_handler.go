package grpc

import (
	"context"

	dataexportv1 "github.com/7akoom/ride-platform/gen/go/ride/dataexport/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PersonalDataFunc reads what this service keeps about one person.
type PersonalDataFunc func(ctx context.Context, identityID, riderID, driverID string) ([]*dataexportv1.DataSection, error)

// WithPersonalData answers ExportPersonalData (identity-service's data export).
func (h *RiderHandler) WithPersonalData(export PersonalDataFunc) *RiderHandler {
	h.personalData = export

	return h
}

// ExportPersonalData is internal only (the interceptor lets nothing but the
// internal service token through).
func (h *RiderHandler) ExportPersonalData(
	ctx context.Context,
	request *dataexportv1.ExportPersonalDataRequest,
) (*dataexportv1.ExportPersonalDataResponse, error) {
	if h.personalData == nil {
		return nil, status.Error(codes.Unimplemented, "personal data export is not available")
	}

	if request.GetIdentityId() == "" {
		return nil, status.Error(codes.InvalidArgument, "identity_id is required")
	}

	sections, err := h.personalData(ctx, request.GetIdentityId(), request.GetRiderId(), request.GetDriverId())
	if err != nil {
		h.logger.Error("personal data export failed", "error", err)

		return nil, status.Error(codes.Internal, "failed to export personal data")
	}

	return &dataexportv1.ExportPersonalDataResponse{Sections: sections}, nil
}
