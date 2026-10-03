package grpc

import (
	"context"
	"errors"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/earnings"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/incentives"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// WithEarnings gives drivers their earnings summary.
func (h *WalletHandler) WithEarnings(service *earnings.Service) *WalletHandler {
	h.earnings = service

	return h
}

// WithIncentives gives staff incentive campaigns and drivers their progress.
func (h *WalletHandler) WithIncentives(service *incentives.Service) *WalletHandler {
	h.incentives = service

	return h
}

func (h *WalletHandler) GetDriverEarnings(ctx context.Context, request *walletv1.GetDriverEarningsRequest) (*walletv1.DriverEarnings, error) {
	if h.earnings == nil {
		return nil, status.Error(codes.Unimplemented, "earnings are not available")
	}

	period := earnings.PeriodDay

	switch request.GetPeriod() {
	case walletv1.EarningsPeriod_EARNINGS_PERIOD_UNSPECIFIED, walletv1.EarningsPeriod_EARNINGS_PERIOD_DAY:
	case walletv1.EarningsPeriod_EARNINGS_PERIOD_WEEK:
		period = earnings.PeriodWeek
	case walletv1.EarningsPeriod_EARNINGS_PERIOD_MONTH:
		period = earnings.PeriodMonth
	default:
		return nil, status.Error(codes.InvalidArgument, earnings.ErrInvalidPeriod.Error())
	}

	report, err := h.earnings.Get(ctx, request.GetDriverId(), period, request.GetDate())
	if err != nil {
		switch {
		case errors.Is(err, earnings.ErrDriverNotFound):
			return nil, status.Error(codes.NotFound, "driver not found")
		case errors.Is(err, earnings.ErrInvalidDate), errors.Is(err, earnings.ErrInvalidPeriod):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}

		h.logger.Error("earnings failed", "error", err)

		return nil, status.Error(codes.Internal, "failed to sum earnings")
	}

	response := &walletv1.DriverEarnings{
		DriverId:     report.DriverID,
		CurrencyCode: report.CurrencyCode,
		Period:       request.GetPeriod(),
		FromDate:     report.FromDate,
		ToDate:       report.ToDate,
		TimeZone:     report.TimeZone,
		Totals:       toProtoTotals(report.Totals),
	}

	if response.Period == walletv1.EarningsPeriod_EARNINGS_PERIOD_UNSPECIFIED {
		response.Period = walletv1.EarningsPeriod_EARNINGS_PERIOD_DAY
	}

	for _, day := range report.Days {
		response.Days = append(response.Days, &walletv1.EarningsDay{Date: day.Date, Totals: toProtoTotals(day.Totals)})
	}

	return response, nil
}

func toProtoTotals(t earnings.Totals) *walletv1.EarningsTotals {
	return &walletv1.EarningsTotals{
		Trips:         int32(t.Trips),
		Fares:         t.Fares.String(),
		Commission:    t.Commission.String(),
		TripEarnings:  t.TripEarnings.String(),
		Fees:          t.Fees.String(),
		Tips:          t.Tips.String(),
		Incentives:    t.Incentives.String(),
		Refunds:       t.Refunds.String(),
		Adjustments:   t.Adjustments.String(),
		NetEarnings:   t.Net().String(),
		CashCollected: t.CashCollected.String(),
		WalletPaid:    t.WalletPaid.String(),
	}
}

func (h *WalletHandler) CreateIncentiveCampaign(ctx context.Context, request *walletv1.CreateIncentiveCampaignRequest) (*walletv1.IncentiveCampaignResponse, error) {
	if h.incentives == nil {
		return nil, status.Error(codes.Unimplemented, "incentives are not available")
	}

	tiers := make([]incentives.Tier, 0, len(request.GetTiers()))

	for _, t := range request.GetTiers() {
		amount, err := decimal.NewFromString(t.GetAmount())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, incentives.ErrInvalidTiers.Error())
		}

		tiers = append(tiers, incentives.Tier{Trips: int(t.GetTrips()), Amount: amount})
	}

	in := incentives.CreateInput{
		Name:                request.GetName(),
		Description:         request.GetDescription(),
		CityID:              request.GetCityId(),
		ZoneIDs:             request.GetZoneIds(),
		VehicleClass:        request.GetVehicleClass(),
		DailyStart:          request.GetDailyStart(),
		DailyEnd:            request.GetDailyEnd(),
		TimeZone:            request.GetTimeZone(),
		MinAcceptanceRate:   request.GetMinAcceptanceRate(),
		MaxCancellationRate: request.GetMaxCancellationRate(),
		MinRating:           request.GetMinRating(),
		Tiers:               tiers,
		IdempotencyKey:      request.GetIdempotencyKey(),
	}

	if request.GetStartsAt() != nil {
		in.StartsAt = request.GetStartsAt().AsTime()
	}

	if request.GetEndsAt() != nil {
		in.EndsAt = request.GetEndsAt().AsTime()
	}

	if principal, ok := authenticatedPrincipalFromContext(ctx); ok && principal.IdentityID != internalServicePrincipalID {
		in.CreatedBy = principal.IdentityID
	}

	created, err := h.incentives.Create(ctx, in)
	if err != nil {
		return nil, h.mapIncentiveError(err)
	}

	return &walletv1.IncentiveCampaignResponse{Campaign: h.toProtoCampaign(created)}, nil
}

func (h *WalletHandler) ListIncentiveCampaigns(ctx context.Context, request *walletv1.ListIncentiveCampaignsRequest) (*walletv1.ListIncentiveCampaignsResponse, error) {
	if h.incentives == nil {
		return nil, status.Error(codes.Unimplemented, "incentives are not available")
	}

	state, ok := campaignStateOf(request.GetStatus())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, incentives.ErrInvalidStatus.Error())
	}

	page, err := h.incentives.List(ctx, state, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapIncentiveError(err)
	}

	response := &walletv1.ListIncentiveCampaignsResponse{NextPageToken: page.NextPageToken}
	for _, c := range page.Campaigns {
		response.Campaigns = append(response.Campaigns, h.toProtoCampaign(c))
	}

	return response, nil
}

func (h *WalletHandler) GetIncentiveCampaign(ctx context.Context, request *walletv1.GetIncentiveCampaignRequest) (*walletv1.IncentiveCampaignResponse, error) {
	if h.incentives == nil {
		return nil, status.Error(codes.Unimplemented, "incentives are not available")
	}

	found, err := h.incentives.Get(ctx, request.GetCampaignId())
	if err != nil {
		return nil, h.mapIncentiveError(err)
	}

	return &walletv1.IncentiveCampaignResponse{Campaign: h.toProtoCampaign(found)}, nil
}

func (h *WalletHandler) CancelIncentiveCampaign(ctx context.Context, request *walletv1.CancelIncentiveCampaignRequest) (*walletv1.IncentiveCampaignResponse, error) {
	if h.incentives == nil {
		return nil, status.Error(codes.Unimplemented, "incentives are not available")
	}

	cancelled, err := h.incentives.Cancel(ctx, request.GetCampaignId(), request.GetReason())
	if err != nil {
		return nil, h.mapIncentiveError(err)
	}

	return &walletv1.IncentiveCampaignResponse{Campaign: h.toProtoCampaign(cancelled)}, nil
}

func (h *WalletHandler) ListIncentivePayouts(ctx context.Context, request *walletv1.ListIncentivePayoutsRequest) (*walletv1.ListIncentivePayoutsResponse, error) {
	if h.incentives == nil {
		return nil, status.Error(codes.Unimplemented, "incentives are not available")
	}

	page, err := h.incentives.Payouts(ctx, request.GetCampaignId(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapIncentiveError(err)
	}

	response := &walletv1.ListIncentivePayoutsResponse{NextPageToken: page.NextPageToken}
	for _, p := range page.Payouts {
		response.Payouts = append(response.Payouts, toProtoIncentivePayout(p))
	}

	return response, nil
}

func (h *WalletHandler) ListDriverIncentives(ctx context.Context, request *walletv1.ListDriverIncentivesRequest) (*walletv1.ListDriverIncentivesResponse, error) {
	if h.incentives == nil {
		return nil, status.Error(codes.Unimplemented, "incentives are not available")
	}

	views, err := h.incentives.ForDriver(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapIncentiveError(err)
	}

	response := &walletv1.ListDriverIncentivesResponse{}

	for _, v := range views {
		item := &walletv1.DriverIncentive{
			Campaign:         h.toProtoCampaign(v.Campaign),
			CompletedTrips:   int32(v.CompletedTrips),
			AcceptanceRate:   v.AcceptanceRate.StringFixed(2),
			CancellationRate: v.CancellationRate.StringFixed(2),
			Unmet:            v.Unmet,
		}

		if v.Reached != nil {
			item.ReachedTierTrips, item.ReachedAmount = int32(v.Reached.Trips), v.Reached.Amount.String()
		}

		if v.Next != nil {
			item.NextTierTrips, item.NextTierAmount = int32(v.Next.Trips), v.Next.Amount.String()
		}

		if v.Payout != nil {
			item.Payout = toProtoIncentivePayout(*v.Payout)
		}

		response.Incentives = append(response.Incentives, item)
	}

	return response, nil
}

func (h *WalletHandler) mapIncentiveError(err error) error {
	switch {
	case errors.Is(err, incentives.ErrCampaignNotFound):
		return status.Error(codes.NotFound, "incentive campaign not found")
	case errors.Is(err, incentives.ErrDriverNotFound):
		return status.Error(codes.NotFound, "driver not found")
	case errors.Is(err, incentives.ErrNotCancellable):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, incentives.ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, incentives.ErrInvalidName),
		errors.Is(err, incentives.ErrInvalidPeriod),
		errors.Is(err, incentives.ErrInvalidScope),
		errors.Is(err, incentives.ErrInvalidHours),
		errors.Is(err, incentives.ErrInvalidTimeZone),
		errors.Is(err, incentives.ErrInvalidConditions),
		errors.Is(err, incentives.ErrInvalidTiers),
		errors.Is(err, incentives.ErrIdempotencyKey),
		errors.Is(err, incentives.ErrReasonRequired),
		errors.Is(err, incentives.ErrInvalidPage),
		errors.Is(err, incentives.ErrInvalidStatus):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		h.logger.Error("incentive request failed", "error", err)

		return status.Error(codes.Internal, "failed to process incentive request")
	}
}

func campaignStateOf(s walletv1.IncentiveCampaignStatus) (incentives.State, bool) {
	switch s {
	case walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_UNSPECIFIED:
		return "", true
	case walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_SCHEDULED:
		return incentives.StateScheduled, true
	case walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_RUNNING:
		return incentives.StateRunning, true
	case walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_SETTLING:
		return incentives.StateSettling, true
	case walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_SETTLED:
		return incentives.StateSettled, true
	case walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_CANCELLED:
		return incentives.StateCancelled, true
	default:
		return "", false
	}
}

func (h *WalletHandler) toProtoCampaign(c incentives.Campaign) *walletv1.IncentiveCampaign {
	out := &walletv1.IncentiveCampaign{
		Id:           c.ID,
		Name:         c.Name,
		Description:  c.Description,
		CurrencyCode: c.CurrencyCode,
		StartsAt:     timestamppb.New(c.StartsAt),
		EndsAt:       timestamppb.New(c.EndsAt),
		CityId:       c.CityID,
		ZoneIds:      c.ZoneIDs,
		VehicleClass: c.VehicleClass,
		TimeZone:     c.TimeZone,
		CancelReason: c.CancelReason,
		CreatedAt:    timestamppb.New(c.CreatedAt),
		PaidDrivers:  int32(c.PaidDrivers),
		PaidTotal:    c.PaidTotal.String(),
	}

	if c.DailyStart != c.DailyEnd {
		out.DailyStart, out.DailyEnd = incentives.ClockOf(c.DailyStart), incentives.ClockOf(c.DailyEnd)
	}

	out.MinAcceptanceRate = optionalMoney(c.MinAcceptanceRate)
	out.MaxCancellationRate = optionalMoney(c.MaxCancellationRate)
	out.MinRating = optionalMoney(c.MinRating)

	for _, t := range c.Tiers {
		out.Tiers = append(out.Tiers, &walletv1.IncentiveTier{Trips: int32(t.Trips), Amount: t.Amount.String()})
	}

	if !c.SettledAt.IsZero() {
		out.SettledAt = timestamppb.New(c.SettledAt)
	}

	switch c.StateAt(h.incentives.Now()) {
	case incentives.StateScheduled:
		out.Status = walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_SCHEDULED
	case incentives.StateRunning:
		out.Status = walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_RUNNING
	case incentives.StateSettling:
		out.Status = walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_SETTLING
	case incentives.StateSettled:
		out.Status = walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_SETTLED
	case incentives.StateCancelled:
		out.Status = walletv1.IncentiveCampaignStatus_INCENTIVE_CAMPAIGN_STATUS_CANCELLED
	}

	return out
}

func optionalMoney(m *wallet.Money) string {
	if m == nil {
		return ""
	}

	return m.String()
}

func toProtoIncentivePayout(p incentives.Payout) *walletv1.IncentivePayout {
	out := &walletv1.IncentivePayout{
		CampaignId:       p.CampaignID,
		DriverId:         p.DriverID,
		CompletedTrips:   int32(p.CompletedTrips),
		AcceptanceRate:   p.AcceptanceRate.StringFixed(2),
		CancellationRate: p.CancellationRate.StringFixed(2),
		Rating:           optionalMoney(p.Rating),
		TierTrips:        int32(p.TierTrips),
		Amount:           p.Amount.String(),
		Unmet:            p.Unmet,
		CreatedAt:        timestamppb.New(p.CreatedAt),
		Status:           walletv1.IncentivePayoutStatus_INCENTIVE_PAYOUT_STATUS_NOT_ELIGIBLE,
	}

	if p.Status == incentives.PayoutPaid {
		out.Status = walletv1.IncentivePayoutStatus_INCENTIVE_PAYOUT_STATUS_PAID
	}

	return out
}
