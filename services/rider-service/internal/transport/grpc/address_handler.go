package grpc

import (
	"context"
	"errors"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/address"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WithAddresses makes the handler serve saved addresses. Until it is called
// those RPCs answer UNIMPLEMENTED.
func (h *RiderHandler) WithAddresses(service *address.Service) *RiderHandler {
	if service == nil {
		panic("address service is required")
	}

	h.addresses = service

	return h
}

func (h *RiderHandler) addressesReady() error {
	if h.addresses == nil {
		return status.Error(codes.Unimplemented, "saved addresses are not configured")
	}

	return nil
}

func (h *RiderHandler) CreateSavedAddress(
	ctx context.Context,
	request *riderv1.CreateSavedAddressRequest,
) (*riderv1.SavedAddressResponse, error) {
	if err := h.addressesReady(); err != nil {
		return nil, err
	}

	details, err := addressDetails(request.GetKind(), request.GetLabel(), request.GetCoordinates(),
		request.GetAddress(), request.GetDetails(), request.GetNoteForDriver())
	if err != nil {
		return nil, err
	}

	created, err := h.addresses.Create(ctx, request.GetRiderId(), details, request.GetPhotoMediaId())
	if err != nil {
		return nil, h.mapAddressError(err)
	}

	return &riderv1.SavedAddressResponse{Address: toProtoAddress(created)}, nil
}

func (h *RiderHandler) ListSavedAddresses(
	ctx context.Context,
	request *riderv1.ListSavedAddressesRequest,
) (*riderv1.ListSavedAddressesResponse, error) {
	if err := h.addressesReady(); err != nil {
		return nil, err
	}

	found, err := h.addresses.List(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapAddressError(err)
	}

	out := make([]*riderv1.SavedAddress, 0, len(found))
	for _, a := range found {
		out = append(out, toProtoAddress(a))
	}

	return &riderv1.ListSavedAddressesResponse{Addresses: out}, nil
}

func (h *RiderHandler) GetSavedAddress(
	ctx context.Context,
	request *riderv1.GetSavedAddressRequest,
) (*riderv1.SavedAddressResponse, error) {
	if err := h.addressesReady(); err != nil {
		return nil, err
	}

	found, err := h.addresses.Get(ctx, request.GetRiderId(), request.GetAddressId())
	if err != nil {
		return nil, h.mapAddressError(err)
	}

	return &riderv1.SavedAddressResponse{Address: toProtoAddress(found)}, nil
}

func (h *RiderHandler) UpdateSavedAddress(
	ctx context.Context,
	request *riderv1.UpdateSavedAddressRequest,
) (*riderv1.SavedAddressResponse, error) {
	if err := h.addressesReady(); err != nil {
		return nil, err
	}

	details, err := addressDetails(request.GetKind(), request.GetLabel(), request.GetCoordinates(),
		request.GetAddress(), request.GetDetails(), request.GetNoteForDriver())
	if err != nil {
		return nil, err
	}

	updated, err := h.addresses.Update(ctx, request.GetRiderId(), request.GetAddressId(), details,
		request.GetPhotoMediaId(), request.GetRemovePhoto())
	if err != nil {
		return nil, h.mapAddressError(err)
	}

	return &riderv1.SavedAddressResponse{Address: toProtoAddress(updated)}, nil
}

func (h *RiderHandler) DeleteSavedAddress(
	ctx context.Context,
	request *riderv1.DeleteSavedAddressRequest,
) (*riderv1.DeleteSavedAddressResponse, error) {
	if err := h.addressesReady(); err != nil {
		return nil, err
	}

	if err := h.addresses.Delete(ctx, request.GetRiderId(), request.GetAddressId()); err != nil {
		return nil, h.mapAddressError(err)
	}

	return &riderv1.DeleteSavedAddressResponse{}, nil
}

var kindsFromProto = map[riderv1.SavedAddressKind]address.Kind{
	riderv1.SavedAddressKind_SAVED_ADDRESS_KIND_HOME:  address.KindHome,
	riderv1.SavedAddressKind_SAVED_ADDRESS_KIND_WORK:  address.KindWork,
	riderv1.SavedAddressKind_SAVED_ADDRESS_KIND_OTHER: address.KindOther,
}

func kindToProto(kind address.Kind) riderv1.SavedAddressKind {
	for protoKind, domain := range kindsFromProto {
		if domain == kind {
			return protoKind
		}
	}

	return riderv1.SavedAddressKind_SAVED_ADDRESS_KIND_UNSPECIFIED
}

func addressDetails(
	kind riderv1.SavedAddressKind,
	label string,
	coordinates *riderv1.Coordinates,
	text, details, note string,
) (address.Details, error) {
	domain, ok := kindsFromProto[kind]
	if !ok {
		return address.Details{}, status.Error(codes.InvalidArgument, address.ErrInvalidKind.Error())
	}

	point := address.Coordinates{}
	if coordinates != nil {
		point = address.Coordinates{Latitude: coordinates.GetLatitude(), Longitude: coordinates.GetLongitude()}
	}

	return address.Details{
		Kind:          domain,
		Label:         label,
		Coordinates:   point,
		Address:       text,
		Details:       details,
		NoteForDriver: note,
	}, nil
}

func toProtoAddress(a address.Address) *riderv1.SavedAddress {
	return &riderv1.SavedAddress{
		Id:            a.ID,
		RiderId:       a.RiderID,
		Kind:          kindToProto(a.Kind),
		Label:         a.Label,
		Coordinates:   &riderv1.Coordinates{Latitude: a.Coordinates.Latitude, Longitude: a.Coordinates.Longitude},
		Address:       a.Address,
		Details:       a.Details,
		NoteForDriver: a.NoteForDriver,
		PhotoMediaId:  a.PhotoMediaID,
		CreatedAt:     timestamppb.New(a.CreatedAt),
		UpdatedAt:     timestamppb.New(a.UpdatedAt),
	}
}

func (h *RiderHandler) mapAddressError(err error) error {
	switch {
	case errors.Is(err, address.ErrAddressNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, address.ErrRiderNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, address.ErrKindTaken):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, address.ErrTooManyAddresses):
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, address.ErrPhotoNotUsable):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, address.ErrPhotoUnavailable):
		h.logger.Warn("media-service could not be asked about an address photo", "error", err)

		return status.Error(codes.Unavailable, "the photo could not be checked right now, try again")

	case errors.Is(err, address.ErrAddressIDRequired),
		errors.Is(err, address.ErrInvalidKind),
		errors.Is(err, address.ErrLabelRequired),
		errors.Is(err, address.ErrLabelTooLong),
		errors.Is(err, address.ErrAddressTooLong),
		errors.Is(err, address.ErrDetailsTooLong),
		errors.Is(err, address.ErrNoteTooLong),
		errors.Is(err, address.ErrPointRequired),
		errors.Is(err, address.ErrInvalidLatitude),
		errors.Is(err, address.ErrInvalidLongitude),
		errors.Is(err, address.ErrInvalidPhotoID),
		errors.Is(err, address.ErrPhotoConflict):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified saved address failure", "error", err)

		return status.Error(codes.Internal, "failed to process the saved address request")
	}
}
