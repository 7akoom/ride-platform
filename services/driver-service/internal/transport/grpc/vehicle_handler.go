package grpc

import (
	"context"
	"errors"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/vehicles"
)

func (h *DriverHandler) AddVehicle(ctx context.Context, request *driverv1.AddVehicleRequest) (*driverv1.VehicleResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	added, err := h.vehicles.Add(ctx, request.GetDriverId(), vehicles.Details{
		Make: request.GetMake(), Model: request.GetModel(), Color: request.GetColor(),
		PlateNumber: request.GetPlateNumber(), Year: int(request.GetYear()), Class: request.GetVehicleClass(),
	})
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	return &driverv1.VehicleResponse{Vehicle: toProtoVehicle(added)}, nil
}

func (h *DriverHandler) ListVehicles(ctx context.Context, request *driverv1.ListVehiclesRequest) (*driverv1.ListVehiclesResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.vehicles.List(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	response := &driverv1.ListVehiclesResponse{}
	for _, v := range found {
		response.Vehicles = append(response.Vehicles, toProtoVehicle(v))
	}

	return response, nil
}

func (h *DriverHandler) UpdateVehicle(ctx context.Context, request *driverv1.UpdateVehicleRequest) (*driverv1.VehicleResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	updated, err := h.vehicles.Update(ctx, request.GetDriverId(), request.GetVehicleId(), vehicles.Details{
		Make: request.GetMake(), Model: request.GetModel(), Color: request.GetColor(),
		PlateNumber: request.GetPlateNumber(), Year: int(request.GetYear()), Class: request.GetVehicleClass(),
	})
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	return &driverv1.VehicleResponse{Vehicle: toProtoVehicle(updated)}, nil
}

func (h *DriverHandler) ActivateVehicle(ctx context.Context, request *driverv1.VehicleActionRequest) (*driverv1.VehicleResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	activated, err := h.vehicles.Activate(ctx, request.GetDriverId(), request.GetVehicleId())
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	return &driverv1.VehicleResponse{Vehicle: toProtoVehicle(activated)}, nil
}

func (h *DriverHandler) RetireVehicle(ctx context.Context, request *driverv1.VehicleActionRequest) (*driverv1.VehicleResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	retired, err := h.vehicles.Retire(ctx, request.GetDriverId(), request.GetVehicleId())
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	return &driverv1.VehicleResponse{Vehicle: toProtoVehicle(retired)}, nil
}

func (h *DriverHandler) ListPendingVehicles(ctx context.Context, request *driverv1.ListPendingVehiclesRequest) (*driverv1.ListPendingVehiclesResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	page, err := h.vehicles.Pending(ctx, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	response := &driverv1.ListPendingVehiclesResponse{NextPageToken: page.NextPageToken}
	for _, item := range page.Items {
		response.Vehicles = append(response.Vehicles, &driverv1.PendingVehicle{
			Vehicle:           toProtoVehicle(item.Vehicle),
			DriverDisplayName: item.DriverDisplayName,
			DriverStatus:      toProtoDriverStatus(driver.Status(item.DriverStatus)),
		})
	}

	return response, nil
}

func (h *DriverHandler) ApproveVehicle(ctx context.Context, request *driverv1.ApproveVehicleRequest) (*driverv1.VehicleResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	approved, err := h.vehicles.Approve(ctx, vehicles.ApproveInput{
		VehicleID: request.GetVehicleId(), Year: int(request.GetYear()), Class: request.GetVehicleClass(),
		ReviewedBy: reviewerOf(ctx),
	})
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	return &driverv1.VehicleResponse{Vehicle: toProtoVehicle(approved)}, nil
}

func (h *DriverHandler) RejectVehicle(ctx context.Context, request *driverv1.RejectVehicleRequest) (*driverv1.VehicleResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	rejected, err := h.vehicles.Reject(ctx, vehicles.RejectInput{
		VehicleID: request.GetVehicleId(), Reason: request.GetReason(), ReviewedBy: reviewerOf(ctx),
	})
	if err != nil {
		return nil, h.mapVehicleError(err)
	}

	return &driverv1.VehicleResponse{Vehicle: toProtoVehicle(rejected)}, nil
}

func (h *DriverHandler) mapVehicleError(err error) error {
	switch {
	case errors.Is(err, vehicles.ErrDriverNotFound):
		return status.Error(codes.NotFound, "driver not found")
	case errors.Is(err, vehicles.ErrVehicleNotFound):
		return status.Error(codes.NotFound, "vehicle not found")
	case errors.Is(err, vehicles.ErrPlateTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, vehicles.ErrDocumentsIncomplete):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, vehicles.ErrTooManyVehicles),
		errors.Is(err, vehicles.ErrVehicleNotEditable),
		errors.Is(err, vehicles.ErrVehicleNotApproved),
		errors.Is(err, vehicles.ErrDriverNotOffline),
		errors.Is(err, vehicles.ErrVehicleActive),
		errors.Is(err, vehicles.ErrVehicleRetired),
		errors.Is(err, vehicles.ErrVehicleNotPending):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, vehicles.ErrInvalidVehicle),
		errors.Is(err, vehicles.ErrInvalidYear),
		errors.Is(err, vehicles.ErrYearRequired),
		errors.Is(err, vehicles.ErrInvalidClass),
		errors.Is(err, vehicles.ErrReasonRequired),
		errors.Is(err, vehicles.ErrReasonTooLong),
		errors.Is(err, vehicles.ErrInvalidPageToken),
		errors.Is(err, vehicles.ErrInvalidPageSize):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		h.logger.Error("unclassified vehicle failure", "error", err)

		return status.Error(codes.Internal, "failed to process vehicle request")
	}
}

func toProtoVehicle(v vehicles.Vehicle) *driverv1.DriverVehicle {
	out := &driverv1.DriverVehicle{
		Id:              v.ID,
		DriverId:        v.DriverID,
		Make:            v.Make,
		Model:           v.Model,
		Color:           v.Color,
		PlateNumber:     v.PlateNumber,
		Year:            int32(v.Year),
		VehicleClass:    v.Class,
		Status:          toProtoVehicleStatus(v.Status),
		Active:          v.Active,
		RejectionReason: v.RejectionReason,
		CreatedAt:       timestamppb.New(v.CreatedAt),
	}

	if !v.ReviewedAt.IsZero() {
		out.ReviewedAt = timestamppb.New(v.ReviewedAt)
	}

	return out
}

func toProtoVehicleStatus(s vehicles.Status) driverv1.VehicleStatus {
	switch s {
	case vehicles.StatusPending:
		return driverv1.VehicleStatus_VEHICLE_STATUS_PENDING
	case vehicles.StatusApproved:
		return driverv1.VehicleStatus_VEHICLE_STATUS_APPROVED
	case vehicles.StatusRejected:
		return driverv1.VehicleStatus_VEHICLE_STATUS_REJECTED
	case vehicles.StatusRetired:
		return driverv1.VehicleStatus_VEHICLE_STATUS_RETIRED
	default:
		return driverv1.VehicleStatus_VEHICLE_STATUS_UNSPECIFIED
	}
}
