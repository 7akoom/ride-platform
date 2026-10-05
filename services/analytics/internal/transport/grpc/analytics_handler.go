package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	analyticsv1 "github.com/7akoom/ride-platform/gen/go/ride/analytics/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

// AnalyticsHandler adapts query.Service to the generated server: proto <->
// domain conversion only.
type AnalyticsHandler struct {
	analyticsv1.UnimplementedAnalyticsServiceServer

	service *query.Service
	logger  *slog.Logger
}

func NewAnalyticsHandler(service *query.Service, logger *slog.Logger) *AnalyticsHandler {
	if logger == nil {
		panic("analytics handler logger is required")
	}

	return &AnalyticsHandler{service: service, logger: logger}
}

func (h *AnalyticsHandler) GetTripFunnel(ctx context.Context, req *analyticsv1.GetTripFunnelRequest) (*analyticsv1.GetTripFunnelResponse, error) {
	days, totals, w, err := h.service.TripFunnel(ctx, toDomainRange(req.GetRange()), toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetTripFunnel", err)
	}

	resp := &analyticsv1.GetTripFunnelResponse{
		Totals:   funnelPointToProto(totals, ""),
		TimeZone: w.Location.String(),
		FromDate: w.From.Format(time.DateOnly),
		ToDate:   w.To.Format(time.DateOnly),
	}
	for _, d := range days {
		resp.Days = append(resp.Days, funnelPointToProto(d, d.Day.Format(time.DateOnly)))
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetCancellationBreakdown(ctx context.Context, req *analyticsv1.GetCancellationBreakdownRequest) (*analyticsv1.GetCancellationBreakdownResponse, error) {
	breakdown, w, err := h.service.Cancellations(ctx, toDomainRange(req.GetRange()), toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetCancellationBreakdown", err)
	}

	resp := &analyticsv1.GetCancellationBreakdownResponse{
		TotalCancellations:      breakdown.TotalCancellations,
		TotalTrips:              breakdown.TotalTrips,
		CancellationRatePercent: breakdown.CancellationRatePct.StringFixed(2),
		RiderNoShows:            breakdown.RiderNoShows,
		TimeZone:                w.Location.String(),
		FromDate:                w.From.Format(time.DateOnly),
		ToDate:                  w.To.Format(time.DateOnly),
	}
	for _, s := range breakdown.ByStage {
		resp.ByStage = append(resp.ByStage, &analyticsv1.CancellationStageCount{Stage: string(s.Stage), Count: s.Count})
	}
	for _, b := range breakdown.ByCancelledBy {
		resp.ByCancelledBy = append(resp.ByCancelledBy, &analyticsv1.CancellationByCount{CancelledBy: b.CancelledBy, Count: b.Count})
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetRevenueSummary(ctx context.Context, req *analyticsv1.GetRevenueSummaryRequest) (*analyticsv1.GetRevenueSummaryResponse, error) {
	days, summary, w, err := h.service.Revenue(ctx, toDomainRange(req.GetRange()), toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetRevenueSummary", err)
	}

	resp := &analyticsv1.GetRevenueSummaryResponse{
		GrossFareTotal:  summary.GrossFareTotal.String(),
		CommissionTotal: summary.CommissionTotal.String(),
		TotalTrips:      summary.TotalTrips,
		FeeTotal:        summary.FeeTotal.String(),
		TotalFees:       summary.TotalFees,
		Currency:        summary.Currency,
		TimeZone:        w.Location.String(),
		FromDate:        w.From.Format(time.DateOnly),
		ToDate:          w.To.Format(time.DateOnly),
	}
	for _, d := range days {
		resp.Days = append(resp.Days, &analyticsv1.RevenueDayPoint{
			Date:            d.Day.Format(time.DateOnly),
			Currency:        d.Currency,
			GrossFareTotal:  d.GrossFareTotal.String(),
			CommissionTotal: d.CommissionTotal.String(),
			TripCount:       d.TripCount,
			FeeTotal:        d.FeeTotal.String(),
			FeeCount:        d.FeeCount,
		})
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetRiderRetention(ctx context.Context, req *analyticsv1.GetRetentionRequest) (*analyticsv1.GetRetentionResponse, error) {
	cohorts, loc, err := h.service.RiderRetention(ctx, req.GetCohortWeeks())
	if err != nil {
		return nil, h.mapError("GetRiderRetention", err)
	}

	return &analyticsv1.GetRetentionResponse{Cohorts: cohortsToProto(cohorts), TimeZone: loc.String()}, nil
}

func (h *AnalyticsHandler) GetDriverRetention(ctx context.Context, req *analyticsv1.GetRetentionRequest) (*analyticsv1.GetRetentionResponse, error) {
	cohorts, loc, err := h.service.DriverRetention(ctx, req.GetCohortWeeks())
	if err != nil {
		return nil, h.mapError("GetDriverRetention", err)
	}

	return &analyticsv1.GetRetentionResponse{Cohorts: cohortsToProto(cohorts), TimeZone: loc.String()}, nil
}

func (h *AnalyticsHandler) HealthCheck(_ context.Context, _ *analyticsv1.HealthCheckRequest) (*analyticsv1.HealthCheckResponse, error) {
	return &analyticsv1.HealthCheckResponse{Status: "ok"}, nil
}

// mapError keeps internal detail out of the reply.
func (h *AnalyticsHandler) mapError(rpc string, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrUnknownCity):
		return status.Error(codes.InvalidArgument, "unknown city_id")
	case errors.Is(err, domain.ErrUpstreamUnavailable):
		return status.Error(codes.Unavailable, "the city's time zone could not be read; try again")
	}

	h.logger.Error("analytics query failed", "rpc", rpc, "error", err)

	return status.Error(codes.Internal, "internal error")
}

func toDomainRange(r *analyticsv1.DateRange) domain.DateRange {
	if r == nil {
		return domain.DateRange{}
	}

	return domain.DateRange{
		Start: timestampToTime(r.GetStartDate()),
		End:   timestampToTime(r.GetEndDate()),
		From:  r.GetFromDate(),
		To:    r.GetToDate(),
	}
}

func toDomainScope(s *analyticsv1.ReportScope) domain.Scope {
	return domain.Scope{CityID: s.GetCityId(), ZoneID: s.GetZoneId(), VehicleClass: s.GetVehicleClass()}
}

func timestampToTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}

	return ts.AsTime()
}

func funnelPointToProto(p domain.FunnelDayPoint, date string) *analyticsv1.FunnelDayPoint {
	return &analyticsv1.FunnelDayPoint{
		Date:           date,
		RequestedCount: p.RequestedCount,
		AcceptedCount:  p.AcceptedCount,
		StartedCount:   p.StartedCount,
		CompletedCount: p.CompletedCount,
		CancelledCount: p.CancelledCount,
	}
}

func cohortsToProto(cohorts []domain.RetentionCohort) []*analyticsv1.RetentionCohort {
	result := make([]*analyticsv1.RetentionCohort, 0, len(cohorts))

	for _, c := range cohorts {
		pcts := make([]string, 0, len(c.RetentionPercentByWeek))
		for _, pct := range c.RetentionPercentByWeek {
			pcts = append(pcts, pct.StringFixed(2))
		}

		result = append(result, &analyticsv1.RetentionCohort{
			CohortWeek:             c.CohortWeek,
			CohortSize:             c.CohortSize,
			RetentionPercentByWeek: pcts,
		})
	}

	return result
}
