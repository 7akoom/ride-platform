package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/activity"
)

// WithActivity lets wallet-service count drivers' activity for incentives.
func WithActivity(service *activity.Service) HandlerOption {
	return func(h *TripHandler) { h.activity = service }
}

func (h *TripHandler) GetDriverActivity(ctx context.Context, request *tripv1.GetDriverActivityRequest) (*tripv1.DriverActivity, error) {
	if h.activity == nil {
		return nil, status.Error(codes.Unimplemented, "driver activity is not available")
	}

	found, err := h.activity.Driver(ctx, request.GetDriverId(), toActivityScope(request.GetScope()))
	if err != nil {
		return nil, h.mapActivityError(err)
	}

	return toProtoActivity(found), nil
}

func (h *TripHandler) ListDriverActivity(ctx context.Context, request *tripv1.ListDriverActivityRequest) (*tripv1.ListDriverActivityResponse, error) {
	if h.activity == nil {
		return nil, status.Error(codes.Unimplemented, "driver activity is not available")
	}

	page, err := h.activity.Drivers(ctx, toActivityScope(request.GetScope()),
		int(request.GetMinCompletedTrips()), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapActivityError(err)
	}

	response := &tripv1.ListDriverActivityResponse{NextPageToken: page.NextPageToken}
	for _, a := range page.Drivers {
		response.Drivers = append(response.Drivers, toProtoActivity(a))
	}

	return response, nil
}

func (h *TripHandler) mapActivityError(err error) error {
	switch {
	case errors.Is(err, activity.ErrInvalidPeriod),
		errors.Is(err, activity.ErrInvalidScope),
		errors.Is(err, activity.ErrInvalidHours),
		errors.Is(err, activity.ErrInvalidTimeZone),
		errors.Is(err, activity.ErrDriverIDRequired),
		errors.Is(err, activity.ErrInvalidPage):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		h.logger.Error("driver activity failed", "error", err)

		return status.Error(codes.Internal, "failed to count driver activity")
	}
}

func toActivityScope(s *tripv1.ActivityScope) activity.Scope {
	if s == nil {
		return activity.Scope{}
	}

	scope := activity.Scope{
		CityID:       s.GetCityId(),
		ZoneIDs:      s.GetZoneIds(),
		VehicleClass: s.GetVehicleClass(),
		DailyStart:   int(s.GetDailyStartMinute()),
		DailyEnd:     int(s.GetDailyEndMinute()),
		TimeZone:     s.GetTimeZone(),
	}

	if s.GetFrom() != nil {
		scope.From = s.GetFrom().AsTime()
	}

	if s.GetTo() != nil {
		scope.To = s.GetTo().AsTime()
	}

	return scope
}

func toProtoActivity(a activity.Activity) *tripv1.DriverActivity {
	return &tripv1.DriverActivity{
		DriverId:            a.DriverID,
		CompletedTrips:      int32(a.CompletedTrips),
		OffersAccepted:      int32(a.OffersAccepted),
		OffersDeclined:      int32(a.OffersDeclined),
		DriverCancellations: int32(a.DriverCancellations),
	}
}
