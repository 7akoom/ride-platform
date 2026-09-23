package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type DriverHandler struct {
	driverv1.UnimplementedDriverServiceServer

	driverService driver.Service
	logger        *slog.Logger
}

func NewDriverHandler(
	driverService driver.Service,
	logger *slog.Logger,
) *DriverHandler {
	if driverService == nil {
		panic("driver service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &DriverHandler{
		driverService: driverService,
		logger:        logger,
	}
}

func (h *DriverHandler) CreateDriver(
	ctx context.Context,
	request *driverv1.CreateDriverRequest,
) (*driverv1.CreateDriverResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	vehicle := request.GetVehicle()

	created, err := h.driverService.CreateDriver(
		ctx,
		driver.CreateDriverInput{
			IdentityID:   request.GetIdentityId(),
			DisplayName:  request.GetDisplayName(),
			VehicleMake:  vehicle.GetMake(),
			VehicleModel: vehicle.GetModel(),
			VehicleColor: vehicle.GetColor(),
			VehiclePlate: vehicle.GetPlateNumber(),
			VehicleClass: vehicle.GetVehicleClass(),
		},
	)
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.CreateDriverResponse{
		Driver: toProtoDriver(created),
	}, nil
}

func (h *DriverHandler) GetDriver(
	ctx context.Context,
	request *driverv1.GetDriverRequest,
) (*driverv1.GetDriverResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.driverService.GetDriver(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.GetDriverResponse{
		Driver: toProtoDriver(found),
	}, nil
}

func (h *DriverHandler) GetDriverByIdentity(
	ctx context.Context,
	request *driverv1.GetDriverByIdentityRequest,
) (*driverv1.GetDriverResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.driverService.GetDriverByIdentityID(ctx, request.GetIdentityId())
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.GetDriverResponse{
		Driver: toProtoDriver(found),
	}, nil
}

func (h *DriverHandler) UpdateDriverProfile(
	ctx context.Context,
	request *driverv1.UpdateDriverProfileRequest,
) (*driverv1.UpdateDriverProfileResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	vehicle := request.GetVehicle()

	updated, err := h.driverService.UpdateDriverProfile(
		ctx,
		driver.UpdateDriverProfileInput{
			DriverID:     request.GetDriverId(),
			DisplayName:  request.GetDisplayName(),
			VehicleMake:  vehicle.GetMake(),
			VehicleModel: vehicle.GetModel(),
			VehicleColor: vehicle.GetColor(),
			VehiclePlate: vehicle.GetPlateNumber(),
			VehicleClass: vehicle.GetVehicleClass(),
		},
	)
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.UpdateDriverProfileResponse{
		Driver: toProtoDriver(updated),
	}, nil
}

func (h *DriverHandler) UpdateAvailability(
	ctx context.Context,
	request *driverv1.UpdateAvailabilityRequest,
) (*driverv1.UpdateAvailabilityResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	updated, err := h.driverService.UpdateAvailability(
		ctx,
		driver.UpdateDriverAvailabilityInput{
			DriverID:           request.GetDriverId(),
			AvailabilityStatus: toDomainAvailability(request.GetAvailabilityStatus()),
		},
	)
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.UpdateAvailabilityResponse{
		Driver: toProtoDriver(updated),
	}, nil
}

func (h *DriverHandler) ApproveDriver(
	ctx context.Context,
	request *driverv1.ApproveDriverRequest,
) (*driverv1.ApproveDriverResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	approved, err := h.driverService.ApproveDriver(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.ApproveDriverResponse{
		Driver: toProtoDriver(approved),
	}, nil
}

func (h *DriverHandler) RejectDriver(
	ctx context.Context,
	request *driverv1.RejectDriverRequest,
) (*driverv1.RejectDriverResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	// A staff member must tell the driver why; the internal token (ops
	// scripts, other services) may leave it empty.
	if isStaffCall(ctx) && strings.TrimSpace(request.GetReason()) == "" {
		return nil, status.Error(codes.InvalidArgument, "a rejection reason is required")
	}

	rejected, err := h.driverService.RejectDriver(ctx, request.GetDriverId(), request.GetReason())
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	return &driverv1.RejectDriverResponse{
		Driver: toProtoDriver(rejected),
	}, nil
}

func (h *DriverHandler) ListDrivers(
	ctx context.Context,
	request *driverv1.ListDriversRequest,
) (*driverv1.ListDriversResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	statusFilter, ok := toDomainStatus(request.GetStatus())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "status is not valid")
	}

	page, err := h.driverService.ListDrivers(ctx, driver.ListDriversQuery{
		Status:    statusFilter,
		PageSize:  int(request.GetPageSize()),
		PageToken: request.GetPageToken(),
	})
	if err != nil {
		return nil, h.mapDriverError(err)
	}

	response := &driverv1.ListDriversResponse{NextPageToken: page.NextPageToken}
	for _, found := range page.Drivers {
		response.Drivers = append(response.Drivers, toProtoDriver(found))
	}

	return response, nil
}

func toDomainStatus(value driverv1.DriverStatus) (driver.Status, bool) {
	switch value {
	case driverv1.DriverStatus_DRIVER_STATUS_UNSPECIFIED:
		return "", true
	case driverv1.DriverStatus_DRIVER_STATUS_PENDING:
		return driver.StatusPending, true
	case driverv1.DriverStatus_DRIVER_STATUS_ACTIVE:
		return driver.StatusActive, true
	case driverv1.DriverStatus_DRIVER_STATUS_REJECTED:
		return driver.StatusRejected, true
	case driverv1.DriverStatus_DRIVER_STATUS_SUSPENDED:
		return driver.StatusSuspended, true
	default:
		return "", false
	}
}

func (h *DriverHandler) mapDriverError(err error) error {
	switch {
	case errors.Is(err, driver.ErrDriverNotFound):
		return status.Error(codes.NotFound, "driver not found")

	case errors.Is(err, driver.ErrDriverAlreadyExists):
		return status.Error(codes.AlreadyExists, "driver already exists for this identity")

	case errors.Is(err, driver.ErrPlateNumberTaken):
		return status.Error(codes.AlreadyExists, "vehicle plate number is already registered")

	case errors.Is(err, driver.ErrDriverNotApproved):
		return status.Error(codes.FailedPrecondition, "driver is not approved yet")

	case errors.Is(err, driver.ErrInvalidStatusTransition):
		return status.Error(codes.FailedPrecondition, "driver status cannot be changed this way")

	case errors.Is(err, driver.ErrIdentityIDRequired),
		errors.Is(err, driver.ErrDriverIDRequired),
		errors.Is(err, driver.ErrDisplayNameRequired),
		errors.Is(err, driver.ErrDisplayNameTooLong),
		errors.Is(err, driver.ErrVehicleFieldsRequired),
		errors.Is(err, driver.ErrInvalidVehicleClass),
		errors.Is(err, driver.ErrInvalidAvailability),
		errors.Is(err, driver.ErrRejectionReasonTooLong),
		errors.Is(err, driver.ErrInvalidListQuery),
		errors.Is(err, driver.ErrInvalidPageToken):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified driver request failure", "error", err)

		return status.Error(codes.Internal, "failed to process driver request")
	}
}

func toDomainAvailability(a driverv1.AvailabilityStatus) driver.AvailabilityStatus {
	switch a {
	case driverv1.AvailabilityStatus_AVAILABILITY_STATUS_OFFLINE:
		return driver.AvailabilityOffline
	case driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE:
		return driver.AvailabilityAvailable
	case driverv1.AvailabilityStatus_AVAILABILITY_STATUS_BUSY:
		return driver.AvailabilityBusy
	default:
		return ""
	}
}

func toProtoDriver(d driver.Driver) *driverv1.Driver {
	protoStatus := driverv1.DriverStatus_DRIVER_STATUS_UNSPECIFIED

	switch d.Status {
	case driver.StatusPending:
		protoStatus = driverv1.DriverStatus_DRIVER_STATUS_PENDING
	case driver.StatusActive:
		protoStatus = driverv1.DriverStatus_DRIVER_STATUS_ACTIVE
	case driver.StatusRejected:
		protoStatus = driverv1.DriverStatus_DRIVER_STATUS_REJECTED
	case driver.StatusSuspended:
		protoStatus = driverv1.DriverStatus_DRIVER_STATUS_SUSPENDED
	}

	protoAvailability := driverv1.AvailabilityStatus_AVAILABILITY_STATUS_UNSPECIFIED

	switch d.AvailabilityStatus {
	case driver.AvailabilityOffline:
		protoAvailability = driverv1.AvailabilityStatus_AVAILABILITY_STATUS_OFFLINE
	case driver.AvailabilityAvailable:
		protoAvailability = driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE
	case driver.AvailabilityBusy:
		protoAvailability = driverv1.AvailabilityStatus_AVAILABILITY_STATUS_BUSY
	}

	return &driverv1.Driver{
		Id:                 d.ID,
		IdentityId:         d.IdentityID,
		DisplayName:        d.DisplayName,
		Status:             protoStatus,
		AvailabilityStatus: protoAvailability,
		Vehicle: &driverv1.Vehicle{
			Make:         d.Vehicle.Make,
			Model:        d.Vehicle.Model,
			Color:        d.Vehicle.Color,
			PlateNumber:  d.Vehicle.PlateNumber,
			VehicleClass: string(d.Vehicle.Class),
		},
		RatingAverage:   d.RatingAverage,
		RatingCount:     d.RatingCount,
		RejectionReason: d.RejectionReason,
		CreatedAt:       timestamppb.New(d.CreatedAt),
		UpdatedAt:       timestamppb.New(d.UpdatedAt),
	}
}
