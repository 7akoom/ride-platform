package grpc

import (
	"context"
	"time"

	analyticsv1 "github.com/7akoom/ride-platform/gen/go/ride/analytics/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

func (h *AnalyticsHandler) GetServiceLevels(ctx context.Context, req *analyticsv1.GetServiceLevelsRequest) (*analyticsv1.GetServiceLevelsResponse, error) {
	days, totals, w, err := h.service.ServiceLevels(ctx, toDomainRange(req.GetRange()), toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetServiceLevels", err)
	}

	resp := &analyticsv1.GetServiceLevelsResponse{
		Totals:                serviceLevelToProto(totals, ""),
		CompletionRatePercent: query.CompletionRate(totals).StringFixed(2),
		TimeZone:              w.Location.String(),
		FromDate:              w.From.Format(time.DateOnly),
		ToDate:                w.To.Format(time.DateOnly),
	}
	for _, d := range days {
		resp.Days = append(resp.Days, serviceLevelToProto(d, d.Day.Format(time.DateOnly)))
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetDriverOffers(ctx context.Context, req *analyticsv1.GetDriverOffersRequest) (*analyticsv1.GetDriverOffersResponse, error) {
	days, totals, w, err := h.service.DriverOffers(ctx, toDomainRange(req.GetRange()), toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetDriverOffers", err)
	}

	resp := &analyticsv1.GetDriverOffersResponse{
		Totals:                offerDayToProto(totals, ""),
		AcceptanceRatePercent: query.AcceptanceRate(totals).StringFixed(2),
		TimeZone:              w.Location.String(),
		FromDate:              w.From.Format(time.DateOnly),
		ToDate:                w.To.Format(time.DateOnly),
	}
	for _, d := range days {
		resp.Days = append(resp.Days, offerDayToProto(d, d.Day.Format(time.DateOnly)))
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetRatings(ctx context.Context, req *analyticsv1.GetRatingsRequest) (*analyticsv1.GetRatingsResponse, error) {
	ratings, w, err := h.service.Ratings(ctx, toDomainRange(req.GetRange()), toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetRatings", err)
	}

	return &analyticsv1.GetRatingsResponse{
		Drivers:        ratingToProto(ratings.Drivers),
		Riders:         ratingToProto(ratings.Riders),
		CompletedTrips: ratings.CompletedTrips,
		TimeZone:       w.Location.String(),
		FromDate:       w.From.Format(time.DateOnly),
		ToDate:         w.To.Format(time.DateOnly),
	}, nil
}

func (h *AnalyticsHandler) GetMoneyFlows(ctx context.Context, req *analyticsv1.GetMoneyFlowsRequest) (*analyticsv1.GetMoneyFlowsResponse, error) {
	flows, w, err := h.service.MoneyFlows(ctx, toDomainRange(req.GetRange()))
	if err != nil {
		return nil, h.mapError("GetMoneyFlows", err)
	}

	head := flows.Headline
	resp := &analyticsv1.GetMoneyFlowsResponse{
		Totals: ledgerLinesToProto(flows.Totals),
		Headline: &analyticsv1.MoneyHeadline{
			ToppedUp:         head.ToppedUp.String(),
			VouchersRedeemed: head.VouchersRedeemed.String(),
			Commission:       head.Commission.String(),
			Tips:             head.Tips.String(),
			Refunds:          head.Refunds.String(),
			Incentives:       head.Incentives.String(),
			PaidOut:          head.PaidOut.String(),
			Transferred:      head.Transferred.String(),
			AdjustmentsIn:    head.AdjustmentsIn.String(),
			AdjustmentsOut:   head.AdjustmentsOut.String(),
		},
		Held: &analyticsv1.MoneyHeld{
			RiderBalances:    flows.Held.RiderBalances.String(),
			DriverCredit:     flows.Held.DriverCredit.String(),
			DriverDebt:       flows.Held.DriverDebt.String(),
			SuspendedDrivers: flows.Held.SuspendedDrivers,
			RiderDues:        flows.Held.RiderDues.String(),
		},
		Currency: flows.Currency,
		TimeZone: w.Location.String(),
		FromDate: w.From.Format(time.DateOnly),
		ToDate:   w.To.Format(time.DateOnly),
	}
	for _, d := range flows.Days {
		resp.Days = append(resp.Days, &analyticsv1.MoneyFlowDay{Date: d.Day.Format(time.DateOnly), Lines: ledgerLinesToProto(d.Lines)})
	}

	return resp, nil
}

func (h *AnalyticsHandler) GetLiveOverview(ctx context.Context, req *analyticsv1.GetLiveOverviewRequest) (*analyticsv1.GetLiveOverviewResponse, error) {
	live, loc, err := h.service.LiveOverview(ctx, toDomainScope(req.GetScope()))
	if err != nil {
		return nil, h.mapError("GetLiveOverview", err)
	}

	resp := &analyticsv1.GetLiveOverviewResponse{
		Trips:                &analyticsv1.LiveTrips{Waiting: live.Trips.Waiting, OnTheWay: live.Trips.OnTheWay, InProgress: live.Trips.InProgress},
		Today:                funnelPointToProto(live.Today, live.Today.Day.Format(time.DateOnly)),
		TodayGrossFare:       live.TodayGrossFare.String(),
		Currency:             live.Currency,
		DriversAvailable:     live.Available,
		DriversBusy:          live.Busy,
		DriversOffline:       live.Offline,
		DriversPendingReview: live.PendingReview,
		TimeZone:             loc.String(),
		AsOf:                 timestamppb.New(live.AsOf),
	}
	for _, c := range live.ByClass {
		resp.ByClass = append(resp.ByClass, &analyticsv1.DriverClassSupply{
			VehicleClass: c.VehicleClass, Available: c.Available, Busy: c.Busy, Offline: c.Offline,
		})
	}

	return resp, nil
}

func durationToProto(d domain.DurationStats) *analyticsv1.DurationStats {
	return &analyticsv1.DurationStats{Count: d.Count, AverageSeconds: d.Average, MedianSeconds: d.Median, P90Seconds: d.P90}
}

func serviceLevelToProto(d domain.ServiceLevelDay, date string) *analyticsv1.ServiceLevelDay {
	return &analyticsv1.ServiceLevelDay{
		Date: date, RequestedCount: d.Requested, CompletedCount: d.Completed,
		Match: durationToProto(d.Match), Pickup: durationToProto(d.Pickup), Ride: durationToProto(d.Ride),
	}
}

func offerDayToProto(d domain.OfferDay, date string) *analyticsv1.OfferDay {
	return &analyticsv1.OfferDay{
		Date: date, Offered: d.Offered, Accepted: d.Accepted, Rejected: d.Rejected, Expired: d.Expired, Pending: d.Pending,
	}
}

func ratingToProto(r domain.RatingSummary) *analyticsv1.RatingSummary {
	return &analyticsv1.RatingSummary{Count: r.Count, Average: r.Average.StringFixed(2), ByStars: r.ByStars[:]}
}

func ledgerLinesToProto(lines []domain.LedgerLine) []*analyticsv1.MoneyFlowLine {
	out := make([]*analyticsv1.MoneyFlowLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, &analyticsv1.MoneyFlowLine{
			OwnerType: l.OwnerType, Type: l.Type, Entries: l.Entries, Credited: l.Credited.String(), Debited: l.Debited.String(),
		})
	}

	return out
}
