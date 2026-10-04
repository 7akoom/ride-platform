package grpc

import (
	"context"
	"errors"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/profile"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
)

// WithProfile serves riders' personal details and pictures.
func (h *RiderHandler) WithProfile(service *profile.Service) *RiderHandler {
	if service == nil {
		panic("profile service is required")
	}

	h.profile = service

	return h
}

func (h *RiderHandler) profileReady() error {
	if h.profile == nil {
		return status.Error(codes.Unimplemented, "rider details are not available")
	}

	return nil
}

func (h *RiderHandler) mapProfileError(err error) error {
	switch {
	case errors.Is(err, rider.ErrRiderNotFound):
		return status.Error(codes.NotFound, "rider not found")
	case errors.Is(err, profile.ErrInvalidGender),
		errors.Is(err, profile.ErrInvalidDateOfBirth),
		errors.Is(err, profile.ErrInvalidNationality):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, profile.ErrPhotoNotUsable):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, profile.ErrNoPhoto):
		return status.Error(codes.NotFound, "the rider has no photo")
	case errors.Is(err, profile.ErrMediaUnavailable):
		return status.Error(codes.Unavailable, "files are unavailable, try again")
	default:
		h.logger.Error("unclassified rider details failure", "error", err)

		return status.Error(codes.Internal, "failed to process rider request")
	}
}

func genderToProto(g string) riderv1.Gender {
	switch g {
	case profile.GenderMale:
		return riderv1.Gender_GENDER_MALE
	case profile.GenderFemale:
		return riderv1.Gender_GENDER_FEMALE
	default:
		return riderv1.Gender_GENDER_UNSPECIFIED
	}
}

func genderFromProto(g riderv1.Gender) string {
	switch g {
	case riderv1.Gender_GENDER_MALE:
		return profile.GenderMale
	case riderv1.Gender_GENDER_FEMALE:
		return profile.GenderFemale
	case riderv1.Gender_GENDER_UNSPECIFIED:
		return ""
	default:
		return "invalid"
	}
}

func detailsResponse(d profile.Details) *riderv1.RiderDetailsResponse {
	out := &riderv1.RiderDetails{
		RiderId:     d.RiderID,
		Gender:      genderToProto(d.Fields.Gender),
		DateOfBirth: d.Fields.DateOfBirth,
		Nationality: d.Fields.Nationality,
		HasPhoto:    d.PhotoMediaID != "",
	}

	if !d.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(d.UpdatedAt)
	}

	return &riderv1.RiderDetailsResponse{Details: out}
}

func (h *RiderHandler) GetRiderDetails(ctx context.Context, request *riderv1.GetRiderDetailsRequest) (*riderv1.RiderDetailsResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	details, err := h.profile.Get(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return detailsResponse(details), nil
}

func (h *RiderHandler) UpdateRiderDetails(ctx context.Context, request *riderv1.UpdateRiderDetailsRequest) (*riderv1.RiderDetailsResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	var patch profile.Patch

	if request.Gender != nil {
		g := genderFromProto(request.GetGender())
		patch.Gender = &g
	}

	if request.DateOfBirth != nil {
		d := request.GetDateOfBirth()
		patch.DateOfBirth = &d
	}

	if request.Nationality != nil {
		n := request.GetNationality()
		patch.Nationality = &n
	}

	details, err := h.profile.Update(ctx, request.GetRiderId(), patch)
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return detailsResponse(details), nil
}

func (h *RiderHandler) SetRiderPhoto(ctx context.Context, request *riderv1.SetRiderPhotoRequest) (*riderv1.RiderDetailsResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	details, err := h.profile.SetPhoto(ctx, request.GetRiderId(), request.GetMediaId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return detailsResponse(details), nil
}

func (h *RiderHandler) DeleteRiderPhoto(ctx context.Context, request *riderv1.DeleteRiderPhotoRequest) (*riderv1.RiderDetailsResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	details, err := h.profile.DeletePhoto(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return detailsResponse(details), nil
}

func (h *RiderHandler) GetRiderPhoto(ctx context.Context, request *riderv1.GetRiderPhotoRequest) (*riderv1.PhotoURLResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	url, expires, err := h.profile.PhotoURL(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &riderv1.PhotoURLResponse{Url: url, ExpiresAt: timestamppb.New(expires)}, nil
}
