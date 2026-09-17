package grpc

import (
	"context"
	"log/slog"
	"time"

	analyticsv1 "github.com/7akoom/ride-platform/gen/go/ride/analytics/v1"
	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AnalyticsHandler adapts the query.Service to the generated
// AnalyticsServiceServer interface: proto <-> domain conversion only, no
// business logic of its own.
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
	days, totals, err := h.service.GetTripFunnel(ctx, toDomainRange(req.GetRange()))
	if err != nil {
		return nil, h.mapError("GetTripFunnel", err)
	}

	resp := &analyticsv1.GetTripFunnelResponse{
		Totals: funnelPointToProto(totals),
	}
	for _, d := range days {
		resp.Days = append(resp.Days, funnelPointToProto(d))
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetCancellationBreakdown(ctx context.Context, req *analyticsv1.GetCancellationBreakdownRequest) (*analyticsv1.GetCancellationBreakdownResponse, error) {
	breakdown, err := h.service.GetCancellationBreakdown(ctx, toDomainRange(req.GetRange()))
	if err != nil {
		return nil, h.mapError("GetCancellationBreakdown", err)
	}

	resp := &analyticsv1.GetCancellationBreakdownResponse{
		TotalCancellations:     breakdown.TotalCancellations,
		TotalTrips:              breakdown.TotalTrips,
		CancellationRatePercent: breakdown.CancellationRatePct.String(),
	}
	for _, s := range breakdown.ByStage {
		resp.ByStage = append(resp.ByStage, &analyticsv1.CancellationStageCount{
			Stage: string(s.Stage),
			Count: s.Count,
		})
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetRevenueSummary(ctx context.Context, req *analyticsv1.GetRevenueSummaryRequest) (*analyticsv1.GetRevenueSummaryResponse, error) {
	days, summary, err := h.service.GetRevenueSummary(ctx, toDomainRange(req.GetRange()))
	if err != nil {
		return nil, h.mapError("GetRevenueSummary", err)
	}

	resp := &analyticsv1.GetRevenueSummaryResponse{
		GrossFareTotal:  summary.GrossFareTotal.String(),
		CommissionTotal: summary.CommissionTotal.String(),
		TotalTrips:      summary.TotalTrips,
	}
	for _, d := range days {
		resp.Days = append(resp.Days, &analyticsv1.RevenueDayPoint{
			Date:            d.Day.Format("2006-01-02"),
			Currency:        d.Currency,
			GrossFareTotal:  d.GrossFareTotal.String(),
			CommissionTotal: d.CommissionTotal.String(),
			TripCount:       d.TripCount,
		})
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetRiderRetention(ctx context.Context, req *analyticsv1.GetRetentionRequest) (*analyticsv1.GetRetentionResponse, error) {
	cohorts, err := h.service.GetRiderRetention(ctx, req.GetCohortWeeks())
	if err != nil {
		return nil, h.mapError("GetRiderRetention", err)
	}

	return &analyticsv1.GetRetentionResponse{Cohorts: cohortsToProto(cohorts)}, nil
}

func (h *AnalyticsHandler) GetDriverRetention(ctx context.Context, req *analyticsv1.GetRetentionRequest) (*analyticsv1.GetRetentionResponse, error) {
	cohorts, err := h.service.GetDriverRetention(ctx, req.GetCohortWeeks())
	if err != nil {
		return nil, h.mapError("GetDriverRetention", err)
	}

	return &analyticsv1.GetRetentionResponse{Cohorts: cohortsToProto(cohorts)}, nil
}

func (h *AnalyticsHandler) HealthCheck(_ context.Context, _ *analyticsv1.HealthCheckRequest) (*analyticsv1.HealthCheckResponse, error) {
	return &analyticsv1.HealthCheckResponse{Status: "ok"}, nil
}

// mapError logs the real error server-side (the pattern applied across
// every other handler in this codebase — see e.g. pricing_handler.go) and
// returns a generic Internal status to the caller, so no internal detail
// leaks over the wire.
func (h *AnalyticsHandler) mapError(rpc string, err error) error {
	h.logger.Error("analytics query failed", "rpc", rpc, "error", err)

	return status.Error(codes.Internal, "internal error")
}

// toDomainRange converts the proto DateRange (nil, or either bound nil) into
// domain.DateRange, leaving a zero time.Time on any missing bound — query.
// Service.resolveRange treats a zero bound as "default it".
func toDomainRange(r *analyticsv1.DateRange) domain.DateRange {
	if r == nil {
		return domain.DateRange{}
	}

	return domain.DateRange{
		Start: timestampToTime(r.GetStartDate()),
		End:   timestampToTime(r.GetEndDate()),
	}
}

func timestampToTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}

	return ts.AsTime()
}

func funnelPointToProto(p domain.FunnelDayPoint) *analyticsv1.FunnelDayPoint {
	date := ""
	if !p.Day.IsZero() {
		date = p.Day.Format("2006-01-02")
	}

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
			pcts = append(pcts, pct.String())
		}

		result = append(result, &analyticsv1.RetentionCohort{
			CohortWeek:             c.CohortWeek,
			CohortSize:             c.CohortSize,
			RetentionPercentByWeek: pcts,
		})
	}

	return result
}
