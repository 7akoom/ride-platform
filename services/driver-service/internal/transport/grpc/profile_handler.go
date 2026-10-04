package grpc

import (
	"context"
	"errors"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/profile"
)

// WithProfile serves drivers' personal details, photo and name changes.
func (h *DriverHandler) WithProfile(service *profile.Service) *DriverHandler {
	if service == nil {
		panic("profile service is required")
	}

	h.profile = service

	return h
}

func (h *DriverHandler) profileReady() error {
	if h.profile == nil {
		return status.Error(codes.Unimplemented, "driver details are not available")
	}

	return nil
}

func (h *DriverHandler) mapProfileError(err error) error {
	switch {
	case errors.Is(err, driver.ErrDriverNotFound):
		return status.Error(codes.NotFound, "driver not found")
	case errors.Is(err, profile.ErrNameChangeNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, profile.ErrNoPhoto):
		return status.Error(codes.NotFound, "the driver has no approved profile photo")
	case errors.Is(err, profile.ErrInvalidGender), errors.Is(err, profile.ErrInvalidDateOfBirth),
		errors.Is(err, profile.ErrInvalidNationality), errors.Is(err, profile.ErrInvalidName),
		errors.Is(err, profile.ErrInvalidReason), errors.Is(err, profile.ErrRejectionRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, profile.ErrNameChangeOpen):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, profile.ErrDetailsLocked),
		errors.Is(err, profile.ErrNameChangeNotPending), errors.Is(err, profile.ErrNameUnchanged),
		errors.Is(err, profile.ErrNameChangeNotNeeded):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, profile.ErrMediaUnavailable):
		return status.Error(codes.Unavailable, "files are unavailable, try again")
	default:
		h.logger.Error("unclassified driver details failure", "error", err)

		return status.Error(codes.Internal, "failed to process driver request")
	}
}

func genderToProto(g string) driverv1.Gender {
	switch g {
	case profile.GenderMale:
		return driverv1.Gender_GENDER_MALE
	case profile.GenderFemale:
		return driverv1.Gender_GENDER_FEMALE
	default:
		return driverv1.Gender_GENDER_UNSPECIFIED
	}
}

func genderFromProto(g driverv1.Gender) string {
	switch g {
	case driverv1.Gender_GENDER_MALE:
		return profile.GenderMale
	case driverv1.Gender_GENDER_FEMALE:
		return profile.GenderFemale
	case driverv1.Gender_GENDER_UNSPECIFIED:
		return ""
	default:
		return "invalid"
	}
}

func detailsResponse(d profile.Details) *driverv1.DriverDetailsResponse {
	out := &driverv1.DriverDetails{
		DriverId:    d.DriverID,
		Gender:      genderToProto(d.Fields.Gender),
		DateOfBirth: d.Fields.DateOfBirth,
		Nationality: d.Fields.Nationality,
		HasPhoto:    d.HasPhoto,
	}

	if !d.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(d.UpdatedAt)
	}

	return &driverv1.DriverDetailsResponse{Details: out}
}

var nameChangeStatuses = map[profile.NameChangeStatus]driverv1.NameChangeStatus{
	profile.NameChangePending:  driverv1.NameChangeStatus_NAME_CHANGE_STATUS_PENDING,
	profile.NameChangeApproved: driverv1.NameChangeStatus_NAME_CHANGE_STATUS_APPROVED,
	profile.NameChangeRejected: driverv1.NameChangeStatus_NAME_CHANGE_STATUS_REJECTED,
}

func nameChangeToProto(c profile.NameChange) *driverv1.NameChange {
	out := &driverv1.NameChange{
		Id:              c.ID,
		DriverId:        c.DriverID,
		CurrentName:     c.CurrentName,
		RequestedName:   c.RequestedName,
		Reason:          c.Reason,
		Status:          nameChangeStatuses[c.Status],
		RejectionReason: c.RejectionReason,
		CreatedAt:       timestamppb.New(c.CreatedAt),
	}

	if c.DecidedAt != nil {
		out.DecidedAt = timestamppb.New(*c.DecidedAt)
	}

	return out
}

func nameChangesToProto(changes []profile.NameChange) []*driverv1.NameChange {
	out := make([]*driverv1.NameChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, nameChangeToProto(c))
	}

	return out
}

func (h *DriverHandler) GetDriverDetails(ctx context.Context, request *driverv1.GetDriverDetailsRequest) (*driverv1.DriverDetailsResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	details, err := h.profile.Get(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return detailsResponse(details), nil
}

func (h *DriverHandler) UpdateDriverDetails(ctx context.Context, request *driverv1.UpdateDriverDetailsRequest) (*driverv1.DriverDetailsResponse, error) {
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

	details, err := h.profile.Update(ctx, request.GetDriverId(), patch)
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return detailsResponse(details), nil
}

func (h *DriverHandler) GetDriverPhoto(ctx context.Context, request *driverv1.GetDriverPhotoRequest) (*driverv1.DriverPhotoResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	url, expires, err := h.profile.PhotoURL(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &driverv1.DriverPhotoResponse{Url: url, ExpiresAt: timestamppb.New(expires)}, nil
}

func (h *DriverHandler) RequestNameChange(ctx context.Context, request *driverv1.RequestNameChangeRequest) (*driverv1.NameChangeResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	change, err := h.profile.RequestNameChange(ctx, request.GetDriverId(), request.GetRequestedName(), request.GetReason())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &driverv1.NameChangeResponse{NameChange: nameChangeToProto(change)}, nil
}

func (h *DriverHandler) ListNameChanges(ctx context.Context, request *driverv1.ListNameChangesRequest) (*driverv1.ListNameChangesResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	changes, err := h.profile.ListNameChanges(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &driverv1.ListNameChangesResponse{NameChanges: nameChangesToProto(changes)}, nil
}

func (h *DriverHandler) ListPendingNameChanges(ctx context.Context, request *driverv1.ListPendingNameChangesRequest) (*driverv1.ListNameChangesResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	changes, err := h.profile.ListPendingNameChanges(ctx, int(request.GetPageSize()))
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &driverv1.ListNameChangesResponse{NameChanges: nameChangesToProto(changes)}, nil
}

func (h *DriverHandler) ApproveNameChange(ctx context.Context, request *driverv1.ApproveNameChangeRequest) (*driverv1.NameChangeResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	change, err := h.profile.ApproveNameChange(ctx, request.GetNameChangeId(), reviewerOf(ctx))
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &driverv1.NameChangeResponse{NameChange: nameChangeToProto(change)}, nil
}

func (h *DriverHandler) RejectNameChange(ctx context.Context, request *driverv1.RejectNameChangeRequest) (*driverv1.NameChangeResponse, error) {
	if err := h.profileReady(); err != nil {
		return nil, err
	}

	change, err := h.profile.RejectNameChange(ctx, request.GetNameChangeId(), reviewerOf(ctx), request.GetReason())
	if err != nil {
		return nil, h.mapProfileError(err)
	}

	return &driverv1.NameChangeResponse{NameChange: nameChangeToProto(change)}, nil
}
