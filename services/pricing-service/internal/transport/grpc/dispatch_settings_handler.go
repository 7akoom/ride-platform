package grpc

import (
	"context"
	"errors"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/dispatchsettings"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WithDispatchSettings lets the handler serve the dispatch settings.
func (h *PricingHandler) WithDispatchSettings(service *dispatchsettings.Service) *PricingHandler {
	h.dispatchSettings = service

	return h
}

func (h *PricingHandler) GetDispatchSettings(
	ctx context.Context,
	_ *pricingv1.GetDispatchSettingsRequest,
) (*pricingv1.DispatchSettingsResponse, error) {
	if h.dispatchSettings == nil {
		return nil, status.Error(codes.Unimplemented, dispatchsettings.ErrUnavailable.Error())
	}

	settings, err := h.dispatchSettings.Get(ctx)
	if err != nil {
		return nil, h.mapDispatchSettingsError(err)
	}

	return &pricingv1.DispatchSettingsResponse{Settings: toProtoDispatchSettings(settings)}, nil
}

func (h *PricingHandler) UpdateDispatchSettings(
	ctx context.Context,
	request *pricingv1.UpdateDispatchSettingsRequest,
) (*pricingv1.DispatchSettingsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if h.dispatchSettings == nil {
		return nil, status.Error(codes.Unimplemented, dispatchsettings.ErrUnavailable.Error())
	}

	settings, err := h.dispatchSettings.Update(ctx, request.GetNextTripsEnabled(), int(request.GetNextTripRadiusMeters()), staffIdentity(ctx))
	if err != nil {
		return nil, h.mapDispatchSettingsError(err)
	}

	return &pricingv1.DispatchSettingsResponse{Settings: toProtoDispatchSettings(settings)}, nil
}

func (h *PricingHandler) mapDispatchSettingsError(err error) error {
	if errors.Is(err, dispatchsettings.ErrInvalidRadius) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	h.logger.Error("dispatch settings request failed", "error", err)

	return status.Error(codes.Internal, "failed to process dispatch settings")
}

func toProtoDispatchSettings(s dispatchsettings.Settings) *pricingv1.DispatchSettings {
	settings := &pricingv1.DispatchSettings{
		NextTripsEnabled:     s.NextTripsEnabled,
		NextTripRadiusMeters: int32(s.NextTripRadiusMeters),
		UpdatedBy:            s.UpdatedBy,
	}

	if !s.UpdatedAt.IsZero() {
		settings.UpdatedAt = timestamppb.New(s.UpdatedAt)
	}

	return settings
}
