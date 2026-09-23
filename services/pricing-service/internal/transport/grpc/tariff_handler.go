package grpc

import (
	"context"
	"errors"
	"strings"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/tariffs"
)

// --- rate cards --------------------------------------------------------------

func (h *PricingHandler) ListRateCards(
	ctx context.Context,
	request *pricingv1.ListRateCardsRequest,
) (*pricingv1.ListRateCardsResponse, error) {
	cards, err := h.tariffs.ListRateCards(ctx, tariffs.Filter{CityID: request.GetCityId(), ZoneID: request.GetZoneId()})
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	response := &pricingv1.ListRateCardsResponse{}
	for _, card := range cards {
		response.RateCards = append(response.RateCards, toProtoRateCard(card))
	}

	return response, nil
}

func (h *PricingHandler) SetRateCard(
	ctx context.Context,
	request *pricingv1.SetRateCardRequest,
) (*pricingv1.RateCardResponse, error) {
	input := tariffs.RateCardInput{
		Scope: pricing.Scope{
			ZoneID:       request.GetZoneId(),
			CityID:       request.GetCityId(),
			VehicleClass: request.GetVehicleClass(),
		},
		FreeWaitingMinutes:       int(request.GetFreeWaitingMinutes()),
		CancellationGraceMinutes: int(request.GetCancellationGraceMinutes()),
		DemandSurge:              request.GetDemandSurge(),
		WeatherSurge:             request.GetWeatherSurge(),
	}

	for _, field := range []struct {
		name     string
		value    string
		required bool
		into     *decimal.Decimal
	}{
		{"base_fare", request.GetBaseFare(), true, &input.BaseFare},
		{"per_km_rate", request.GetPerKmRate(), true, &input.PerKmRate},
		{"per_minute_rate", request.GetPerMinuteRate(), true, &input.PerMinuteRate},
		{"minimum_fare", request.GetMinimumFare(), false, &input.MinimumFare},
		{"waiting_per_minute", request.GetWaitingPerMinute(), false, &input.WaitingPerMinute},
		{"cancellation_fee", request.GetCancellationFee(), false, &input.CancellationFee},
		{"no_show_fee", request.GetNoShowFee(), false, &input.NoShowFee},
	} {
		amount, err := decimalField(field.name, field.value, field.required)
		if err != nil {
			return nil, err
		}

		*field.into = amount
	}

	if raw := strings.TrimSpace(request.GetMaxSurgePercent()); raw != "" {
		percent, err := decimalField("max_surge_percent", raw, true)
		if err != nil {
			return nil, err
		}

		input.MaxSurgePercent = &percent
	}

	card, err := h.tariffs.SetRateCard(ctx, input, staffIdentity(ctx))
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.RateCardResponse{RateCard: toProtoRateCard(card)}, nil
}

func (h *PricingHandler) RetireRateCard(
	ctx context.Context,
	request *pricingv1.RetireRateCardRequest,
) (*pricingv1.RetireRateCardResponse, error) {
	err := h.tariffs.RetireRateCard(ctx, pricing.Scope{
		ZoneID:       request.GetZoneId(),
		CityID:       request.GetCityId(),
		VehicleClass: request.GetVehicleClass(),
	}, staffIdentity(ctx))
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.RetireRateCardResponse{}, nil
}

// --- surge rules -------------------------------------------------------------

func (h *PricingHandler) ListSurgeRules(
	ctx context.Context,
	request *pricingv1.ListSurgeRulesRequest,
) (*pricingv1.ListSurgeRulesResponse, error) {
	rules, err := h.tariffs.ListSurgeRules(ctx, tariffs.Filter{CityID: request.GetCityId(), ZoneID: request.GetZoneId()})
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	response := &pricingv1.ListSurgeRulesResponse{}
	for _, rule := range rules {
		response.Rules = append(response.Rules, toProtoSurgeRule(rule))
	}

	return response, nil
}

func (h *PricingHandler) CreateSurgeRule(
	ctx context.Context,
	request *pricingv1.CreateSurgeRuleRequest,
) (*pricingv1.SurgeRuleResponse, error) {
	percent, err := decimalField("surge_percent", request.GetSurgePercent(), true)
	if err != nil {
		return nil, err
	}

	rule, err := h.tariffs.CreateSurgeRule(ctx, tariffs.SurgeRuleInput{
		ZoneID:       request.GetZoneId(),
		CityID:       request.GetCityId(),
		Label:        request.GetLabel(),
		DayOfWeek:    optionalDay(request.DayOfWeek),
		StartTime:    request.GetStartTime(),
		EndTime:      request.GetEndTime(),
		SurgePercent: percent,
	})
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.SurgeRuleResponse{Rule: toProtoSurgeRule(rule)}, nil
}

func (h *PricingHandler) UpdateSurgeRule(
	ctx context.Context,
	request *pricingv1.UpdateSurgeRuleRequest,
) (*pricingv1.SurgeRuleResponse, error) {
	percent, err := decimalField("surge_percent", request.GetSurgePercent(), true)
	if err != nil {
		return nil, err
	}

	rule, err := h.tariffs.UpdateSurgeRule(ctx, request.GetRuleId(), tariffs.SurgeRuleInput{
		Label:        request.GetLabel(),
		DayOfWeek:    optionalDay(request.DayOfWeek),
		StartTime:    request.GetStartTime(),
		EndTime:      request.GetEndTime(),
		SurgePercent: percent,
	})
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.SurgeRuleResponse{Rule: toProtoSurgeRule(rule)}, nil
}

func (h *PricingHandler) SetSurgeRuleActive(
	ctx context.Context,
	request *pricingv1.SetSurgeRuleActiveRequest,
) (*pricingv1.SurgeRuleResponse, error) {
	rule, err := h.tariffs.SetSurgeRuleActive(ctx, request.GetRuleId(), request.GetActive())
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.SurgeRuleResponse{Rule: toProtoSurgeRule(rule)}, nil
}

// --- zone surges -------------------------------------------------------------

func (h *PricingHandler) ListZoneSurges(
	ctx context.Context,
	request *pricingv1.ListZoneSurgesRequest,
) (*pricingv1.ListZoneSurgesResponse, error) {
	surges, err := h.tariffs.ListZoneSurges(ctx, request.GetZoneId(), request.GetIncludePast())
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	response := &pricingv1.ListZoneSurgesResponse{}
	for _, surge := range surges {
		response.ZoneSurges = append(response.ZoneSurges, toProtoZoneSurge(surge))
	}

	return response, nil
}

func (h *PricingHandler) CreateZoneSurge(
	ctx context.Context,
	request *pricingv1.CreateZoneSurgeRequest,
) (*pricingv1.ZoneSurgeResponse, error) {
	percent, err := decimalField("surge_percent", request.GetSurgePercent(), true)
	if err != nil {
		return nil, err
	}

	input := tariffs.ZoneSurgeInput{
		ZoneID:          request.GetZoneId(),
		SurgePercent:    percent,
		Reason:          request.GetReason(),
		DurationMinutes: int(request.GetDurationMinutes()),
	}

	if request.GetStartsAt() != nil {
		input.StartsAt = request.GetStartsAt().AsTime()
	}

	surge, err := h.tariffs.CreateZoneSurge(ctx, input, staffIdentity(ctx))
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.ZoneSurgeResponse{ZoneSurge: toProtoZoneSurge(surge)}, nil
}

func (h *PricingHandler) EndZoneSurge(
	ctx context.Context,
	request *pricingv1.EndZoneSurgeRequest,
) (*pricingv1.ZoneSurgeResponse, error) {
	surge, err := h.tariffs.EndZoneSurge(ctx, request.GetZoneSurgeId())
	if err != nil {
		return nil, h.mapTariffError(err)
	}

	return &pricingv1.ZoneSurgeResponse{ZoneSurge: toProtoZoneSurge(surge)}, nil
}

// --- helpers -----------------------------------------------------------------

func (h *PricingHandler) mapTariffError(err error) error {
	var invalidErr *tariffs.InvalidError

	switch {
	case errors.As(err, &invalidErr):
		return status.Error(codes.InvalidArgument, invalidErr.Error())

	case errors.Is(err, tariffs.ErrCityNotFound),
		errors.Is(err, tariffs.ErrZoneNotFound),
		errors.Is(err, tariffs.ErrRateCardNotFound),
		errors.Is(err, tariffs.ErrSurgeRuleNotFound),
		errors.Is(err, tariffs.ErrZoneSurgeNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, tariffs.ErrCannotRetireBase),
		errors.Is(err, tariffs.ErrZoneSurgeOver),
		errors.Is(err, tariffs.ErrNoBaseRateCard):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, tariffs.ErrPlacesUnavailable):
		h.logger.Warn("cities and zones could not be checked", "error", err)

		return status.Error(codes.Unavailable, tariffs.ErrPlacesUnavailable.Error())

	default:
		h.logger.Error("unclassified tariff request failure", "error", err)

		return status.Error(codes.Internal, "failed to process pricing request")
	}
}

// staffIdentity is the identity of the staff member calling, or empty for
// the internal token (a service acting on its own).
func staffIdentity(ctx context.Context) string {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok || principal.IdentityID == internalServicePrincipalID {
		return ""
	}

	return principal.IdentityID
}

func decimalField(name, value string, required bool) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return decimal.Decimal{}, status.Error(codes.InvalidArgument, name+": is required")
		}

		return decimal.Zero, nil
	}

	amount, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Decimal{}, status.Error(codes.InvalidArgument, name+": must be a decimal number")
	}

	return amount, nil
}

func optionalDay(day *int32) *int {
	if day == nil {
		return nil
	}

	value := int(*day)

	return &value
}

func toProtoRateCard(card pricing.Config) *pricingv1.RateCard {
	return &pricingv1.RateCard{
		Id:                       card.ID,
		ZoneId:                   card.ZoneID,
		CityId:                   card.CityID,
		VehicleClass:             card.VehicleClass,
		CurrencyCode:             card.CurrencyCode,
		BaseFare:                 card.BaseFare.String(),
		PerKmRate:                card.PerKmRate.String(),
		PerMinuteRate:            card.PerMinuteRate.String(),
		MinimumFare:              card.MinimumFare.String(),
		FreeWaitingMinutes:       int32(card.FreeWaitingMinutes),
		WaitingPerMinute:         card.WaitingPerMinute.String(),
		CancellationFee:          card.CancellationFee.String(),
		CancellationGraceMinutes: int32(card.CancellationGraceMinutes),
		NoShowFee:                card.NoShowFee.String(),
		MaxSurgePercent:          card.MaxSurgePercent.String(),
		DemandSurge:              card.DemandSurge,
		WeatherSurge:             card.WeatherSurge,
		CreatedAt:                timestamppb.New(card.CreatedAt),
		CreatedBy:                card.CreatedBy,
	}
}

func toProtoSurgeRule(rule pricing.SurgeTimeRule) *pricingv1.SurgeRule {
	out := &pricingv1.SurgeRule{
		Id:           rule.ID,
		Label:        rule.Label,
		ZoneId:       rule.ZoneID,
		CityId:       rule.CityID,
		StartTime:    clockOf(rule.StartTime),
		EndTime:      clockOf(rule.EndTime),
		SurgePercent: rule.SurgePercent.String(),
		Active:       rule.Active,
		CreatedAt:    timestamppb.New(rule.CreatedAt),
		UpdatedAt:    timestamppb.New(rule.UpdatedAt),
	}

	if rule.DayOfWeek != nil {
		day := int32(*rule.DayOfWeek)
		out.DayOfWeek = &day
	}

	return out
}

// clockOf shows "07:00:00" as "07:00", the way staff type it.
func clockOf(value string) string {
	if len(value) == len("15:04:05") && strings.HasSuffix(value, ":00") {
		return value[:5]
	}

	return value
}

func toProtoZoneSurge(surge pricing.ZoneSurge) *pricingv1.ZoneSurge {
	out := &pricingv1.ZoneSurge{
		Id:           surge.ID,
		ZoneId:       surge.ZoneID,
		SurgePercent: surge.SurgePercent.String(),
		Reason:       surge.Reason,
		StartsAt:     timestamppb.New(surge.StartsAt),
		EndsAt:       timestamppb.New(surge.EndsAt),
		CreatedBy:    surge.CreatedBy,
		CreatedAt:    timestamppb.New(surge.CreatedAt),
	}

	if surge.EndedAt != nil {
		out.EndedAt = timestamppb.New(*surge.EndedAt)
	}

	return out
}
