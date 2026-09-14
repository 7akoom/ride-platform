package grpc

import (
	"context"
	"errors"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RiderHandler struct {
	riderv1.UnimplementedRiderServiceServer

	riderService rider.Service
}

func NewRiderHandler(
	riderService rider.Service,
) *RiderHandler {
	if riderService == nil {
		panic("rider service is required")
	}

	return &RiderHandler{
		riderService: riderService,
	}
}

func (h *RiderHandler) CreateRider(
	ctx context.Context,
	request *riderv1.CreateRiderRequest,
) (*riderv1.CreateRiderResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	created, err := h.riderService.CreateRider(
		ctx,
		rider.CreateRiderInput{
			IdentityID:  request.GetIdentityId(),
			DisplayName: request.GetDisplayName(),
		},
	)
	if err != nil {
		return nil, mapRiderError(err)
	}

	return &riderv1.CreateRiderResponse{
		Rider: toProtoRider(created),
	}, nil
}

func (h *RiderHandler) GetRider(
	ctx context.Context,
	request *riderv1.GetRiderRequest,
) (*riderv1.GetRiderResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.riderService.GetRider(ctx, request.GetRiderId())
	if err != nil {
		return nil, mapRiderError(err)
	}

	return &riderv1.GetRiderResponse{
		Rider: toProtoRider(found),
	}, nil
}

func (h *RiderHandler) GetRiderByIdentity(
	ctx context.Context,
	request *riderv1.GetRiderByIdentityRequest,
) (*riderv1.GetRiderResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.riderService.GetRiderByIdentityID(ctx, request.GetIdentityId())
	if err != nil {
		return nil, mapRiderError(err)
	}

	return &riderv1.GetRiderResponse{
		Rider: toProtoRider(found),
	}, nil
}

func (h *RiderHandler) UpdateRiderProfile(
	ctx context.Context,
	request *riderv1.UpdateRiderProfileRequest,
) (*riderv1.UpdateRiderProfileResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	updated, err := h.riderService.UpdateRiderProfile(
		ctx,
		rider.UpdateRiderProfileInput{
			RiderID:     request.GetRiderId(),
			DisplayName: request.GetDisplayName(),
		},
	)
	if err != nil {
		return nil, mapRiderError(err)
	}

	return &riderv1.UpdateRiderProfileResponse{
		Rider: toProtoRider(updated),
	}, nil
}

func mapRiderError(err error) error {
	switch {
	case errors.Is(err, rider.ErrRiderNotFound):
		return status.Error(codes.NotFound, "rider not found")

	case errors.Is(err, rider.ErrRiderAlreadyExists):
		return status.Error(codes.AlreadyExists, "rider already exists for this identity")

	case errors.Is(err, rider.ErrIdentityIDRequired),
		errors.Is(err, rider.ErrRiderIDRequired),
		errors.Is(err, rider.ErrDisplayNameRequired),
		errors.Is(err, rider.ErrDisplayNameTooLong):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return status.Error(codes.Internal, "failed to process rider request")
	}
}

func toProtoRider(r rider.Rider) *riderv1.Rider {
	protoStatus := riderv1.RiderStatus_RIDER_STATUS_UNSPECIFIED

	switch r.Status {
	case rider.StatusActive:
		protoStatus = riderv1.RiderStatus_RIDER_STATUS_ACTIVE
	case rider.StatusSuspended:
		protoStatus = riderv1.RiderStatus_RIDER_STATUS_SUSPENDED
	}

	return &riderv1.Rider{
		Id:            r.ID,
		IdentityId:    r.IdentityID,
		DisplayName:   r.DisplayName,
		Status:        protoStatus,
		RatingAverage: r.RatingAverage,
		RatingCount:   r.RatingCount,
		CreatedAt:     timestamppb.New(r.CreatedAt),
		UpdatedAt:     timestamppb.New(r.UpdatedAt),
	}
}
