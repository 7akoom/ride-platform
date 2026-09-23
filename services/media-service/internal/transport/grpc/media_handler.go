package grpc

import (
	"context"
	"errors"
	"log/slog"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type MediaHandler struct {
	mediav1.UnimplementedMediaServiceServer

	service *media.Service
	logger  *slog.Logger
}

func NewMediaHandler(service *media.Service, logger *slog.Logger) *MediaHandler {
	if service == nil {
		panic("media service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &MediaHandler{service: service, logger: logger}
}

func (h *MediaHandler) CreateUpload(
	ctx context.Context,
	request *mediav1.CreateUploadRequest,
) (*mediav1.CreateUploadResponse, error) {
	// A file always belongs to the person who uploads it: the owner is the
	// caller, never a field of the request, and services do not upload.
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok || principal.IdentityID == internalServicePrincipalID {
		return nil, status.Error(codes.PermissionDenied, "a user's own access token is required")
	}

	purpose, ok := purposeFromProto(request.GetPurpose())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, media.ErrInvalidPurpose.Error())
	}

	created, ticket, err := h.service.CreateUpload(ctx, media.CreateUploadInput{
		OwnerIdentityID: principal.IdentityID,
		Purpose:         purpose,
		ContentType:     request.GetContentType(),
		SizeBytes:       request.GetSizeBytes(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.CreateUploadResponse{
		Media:         toProtoMedia(created),
		UploadUrl:     ticket.URL,
		UploadMethod:  ticket.Method,
		UploadHeaders: ticket.Headers,
		ExpiresAt:     timestamppb.New(ticket.ExpiresAt),
	}, nil
}

func (h *MediaHandler) CompleteUpload(
	ctx context.Context,
	request *mediav1.CompleteUploadRequest,
) (*mediav1.MediaResponse, error) {
	completed, err := h.service.CompleteUpload(ctx, request.GetMediaId())
	if err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.MediaResponse{Media: toProtoMedia(completed)}, nil
}

func (h *MediaHandler) GetMedia(
	ctx context.Context,
	request *mediav1.GetMediaRequest,
) (*mediav1.MediaResponse, error) {
	found, err := h.service.Get(ctx, request.GetMediaId())
	if err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.MediaResponse{Media: toProtoMedia(found)}, nil
}

func (h *MediaHandler) GetDownloadURL(
	ctx context.Context,
	request *mediav1.GetDownloadURLRequest,
) (*mediav1.GetDownloadURLResponse, error) {
	url, expires, err := h.service.DownloadURL(ctx, request.GetMediaId())
	if err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.GetDownloadURLResponse{Url: url, ExpiresAt: timestamppb.New(expires)}, nil
}

func (h *MediaHandler) DeleteMedia(
	ctx context.Context,
	request *mediav1.DeleteMediaRequest,
) (*mediav1.DeleteMediaResponse, error) {
	if err := h.service.Delete(ctx, request.GetMediaId()); err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.DeleteMediaResponse{}, nil
}

func (h *MediaHandler) HoldMedia(
	ctx context.Context,
	request *mediav1.HoldMediaRequest,
) (*mediav1.MediaResponse, error) {
	purpose, ok := purposeFromProto(request.GetPurpose())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, media.ErrInvalidPurpose.Error())
	}

	held, err := h.service.Hold(ctx, request.GetMediaId(), request.GetOwnerIdentityId(), purpose)
	if err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.MediaResponse{Media: toProtoMedia(held)}, nil
}

func (h *MediaHandler) ReleaseMedia(
	ctx context.Context,
	request *mediav1.ReleaseMediaRequest,
) (*mediav1.MediaResponse, error) {
	released, err := h.service.Release(ctx, request.GetMediaId())
	if err != nil {
		return nil, h.mapError(err)
	}

	return &mediav1.MediaResponse{Media: toProtoMedia(released)}, nil
}

func (h *MediaHandler) mapError(err error) error {
	switch {
	case errors.Is(err, media.ErrInvalidPurpose),
		errors.Is(err, media.ErrTypeNotAllowed),
		errors.Is(err, media.ErrInvalidSize),
		errors.Is(err, media.ErrInvalidID):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, media.ErrOwnerRequired):
		return status.Error(codes.PermissionDenied, err.Error())

	case errors.Is(err, media.ErrTooManyPending):
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, media.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, media.ErrNotUploaded),
		errors.Is(err, media.ErrInvalidState),
		errors.Is(err, media.ErrNotReady),
		errors.Is(err, media.ErrHeld),
		errors.Is(err, media.ErrHoldMismatch):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")

	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request timed out")
	}

	h.logger.Error("media request failed", "error", err)

	return status.Error(codes.Internal, "internal error")
}

var purposesFromProto = map[mediav1.MediaPurpose]media.Purpose{
	mediav1.MediaPurpose_MEDIA_PURPOSE_DRIVER_DOCUMENT:    media.PurposeDriverDocument,
	mediav1.MediaPurpose_MEDIA_PURPOSE_PROFILE_PHOTO:      media.PurposeProfilePhoto,
	mediav1.MediaPurpose_MEDIA_PURPOSE_ADDRESS_PHOTO:      media.PurposeAddressPhoto,
	mediav1.MediaPurpose_MEDIA_PURPOSE_SUPPORT_ATTACHMENT: media.PurposeSupportAttachment,
}

func purposeFromProto(purpose mediav1.MediaPurpose) (media.Purpose, bool) {
	found, ok := purposesFromProto[purpose]

	return found, ok
}

func purposeToProto(purpose media.Purpose) mediav1.MediaPurpose {
	for protoPurpose, domainPurpose := range purposesFromProto {
		if domainPurpose == purpose {
			return protoPurpose
		}
	}

	return mediav1.MediaPurpose_MEDIA_PURPOSE_UNSPECIFIED
}

var statusesToProto = map[media.Status]mediav1.MediaStatus{
	media.StatusPending:  mediav1.MediaStatus_MEDIA_STATUS_PENDING,
	media.StatusReady:    mediav1.MediaStatus_MEDIA_STATUS_READY,
	media.StatusRejected: mediav1.MediaStatus_MEDIA_STATUS_REJECTED,
	media.StatusDeleted:  mediav1.MediaStatus_MEDIA_STATUS_DELETED,
	media.StatusExpired:  mediav1.MediaStatus_MEDIA_STATUS_EXPIRED,
}

// toProtoMedia never exposes the object key: files are only reached through
// the short-lived URLs this service signs.
func toProtoMedia(m media.Media) *mediav1.Media {
	contentType, size := m.ContentType, m.SizeBytes
	if m.Status == media.StatusPending {
		contentType, size = m.DeclaredContentType, m.DeclaredSize
	}

	out := &mediav1.Media{
		Id:              m.ID,
		OwnerIdentityId: m.OwnerIdentityID,
		Purpose:         purposeToProto(m.Purpose),
		Status:          statusesToProto[m.Status],
		ContentType:     contentType,
		SizeBytes:       size,
		Sha256:          m.SHA256,
		Width:           int32(m.Width),
		Height:          int32(m.Height),
		RejectionReason: m.RejectionReason,
		Held:            m.Held,
		CreatedAt:       timestamppb.New(m.CreatedAt),
	}

	if m.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*m.CompletedAt)
	}

	return out
}
