package grpc

import (
	"context"
	"errors"
	"log/slog"

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

func (h *DriverHandler) mapDriverError(err error) error {
	switch {
	case errors.Is(err, driver.ErrDriverNotFound):
		return status.Error(codes.NotFound, "driver not found")

	case errors.Is(err, driver.ErrDriverAlreadyExists):
		return status.Error(codes.AlreadyExists, "driver already exists for this identity")

	case errors.Is(err, driver.ErrPlateNumberTaken):
		return status.Error(codes.AlreadyExists, "vehicle plate number is already registered")

	case errors.Is(err, driver.ErrIdentityIDRequired),
		errors.Is(err, driver.ErrDriverIDRequired),
		errors.Is(err, driver.ErrDisplayNameRequired),
		errors.Is(err, driver.ErrDisplayNameTooLong),
		errors.Is(err, driver.ErrVehicleFieldsRequired),
		errors.Is(err, driver.ErrInvalidAvailability):
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
	case driver.StatusActive:
		protoStatus = driverv1.DriverStatus_DRIVER_STATUS_ACTIVE
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
			Make:        d.Vehicle.Make,
			Model:       d.Vehicle.Model,
			Color:       d.Vehicle.Color,
			PlateNumber: d.Vehicle.PlateNumber,
		},
		RatingAverage: d.RatingAverage,
		RatingCount:   d.RatingCount,
		CreatedAt:     timestamppb.New(d.CreatedAt),
		UpdatedAt:     timestamppb.New(d.UpdatedAt),
	}
}
