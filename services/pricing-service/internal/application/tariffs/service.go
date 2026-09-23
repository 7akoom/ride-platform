package tariffs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// Limits on what staff can set. Money fits NUMERIC(12, 4), percents
// NUMERIC(6, 2).
var (
	maxMoney          = decimal.NewFromInt(99_999_999)
	maxSurgePercent   = decimal.NewFromInt(300)
	defaultMaxSurge   = decimal.NewFromInt(150)
	maxMinutesSetting = 60
)

const (
	maxLabelLength          = 80
	maxZoneSurgeMinutes     = 24 * 60
	maxZoneSurgeLeadTime    = 7 * 24 * time.Hour
	zoneSurgeStartTolerance = 5 * time.Minute
)

var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9](:[0-5][0-9])?$`)

// Service is what the staff endpoints call.
type Service struct {
	repository Repository
	places     Places
	now        func() time.Time
}

func NewService(repository Repository, places Places) *Service {
	if repository == nil {
		panic("tariff repository is required")
	}

	if places == nil {
		panic("places are required")
	}

	return &Service{repository: repository, places: places, now: func() time.Time { return time.Now().UTC() }}
}

// --- rate cards --------------------------------------------------------------

func (s *Service) ListRateCards(ctx context.Context, filter Filter) ([]pricing.Config, error) {
	filter, err := checkFilter(filter)
	if err != nil {
		return nil, err
	}

	return s.repository.CurrentRateCards(ctx, filter)
}

// SetRateCard adds a new version of the card for the input's place and
// class. The currency, and the speed and road correction used when OSRM is
// down, come from the base card (every class everywhere): one deployment,
// one currency.
func (s *Service) SetRateCard(ctx context.Context, input RateCardInput, staffID string) (pricing.Config, error) {
	scope, err := s.checkScope(ctx, input.Scope)
	if err != nil {
		return pricing.Config{}, err
	}

	for _, money := range []struct {
		field  string
		amount decimal.Decimal
	}{
		{"base_fare", input.BaseFare},
		{"per_km_rate", input.PerKmRate},
		{"per_minute_rate", input.PerMinuteRate},
		{"minimum_fare", input.MinimumFare},
		{"waiting_per_minute", input.WaitingPerMinute},
		{"cancellation_fee", input.CancellationFee},
		{"no_show_fee", input.NoShowFee},
	} {
		if err := checkMoney(money.field, money.amount); err != nil {
			return pricing.Config{}, err
		}
	}

	if input.FreeWaitingMinutes < 0 || input.FreeWaitingMinutes > maxMinutesSetting {
		return pricing.Config{}, invalid("free_waiting_minutes", "must be between 0 and 60")
	}

	if input.CancellationGraceMinutes < 0 || input.CancellationGraceMinutes > maxMinutesSetting {
		return pricing.Config{}, invalid("cancellation_grace_minutes", "must be between 0 and 60")
	}

	maxSurge := defaultMaxSurge
	if input.MaxSurgePercent != nil {
		maxSurge = *input.MaxSurgePercent
	}

	if err := checkPercent("max_surge_percent", maxSurge, true); err != nil {
		return pricing.Config{}, err
	}

	base, found, err := s.repository.LatestRateCard(ctx, pricing.Scope{})
	if err != nil {
		return pricing.Config{}, fmt.Errorf("read the base rate card: %w", err)
	}

	if !found {
		return pricing.Config{}, ErrNoBaseRateCard
	}

	return s.repository.InsertRateCard(ctx, pricing.Config{
		ZoneID:                   scope.ZoneID,
		CityID:                   scope.CityID,
		VehicleClass:             scope.VehicleClass,
		CurrencyCode:             base.CurrencyCode,
		BaseFare:                 input.BaseFare,
		PerKmRate:                input.PerKmRate,
		PerMinuteRate:            input.PerMinuteRate,
		MinimumFare:              input.MinimumFare,
		FreeWaitingMinutes:       input.FreeWaitingMinutes,
		WaitingPerMinute:         input.WaitingPerMinute,
		CancellationFee:          input.CancellationFee,
		CancellationGraceMinutes: input.CancellationGraceMinutes,
		NoShowFee:                input.NoShowFee,
		MaxSurgePercent:          maxSurge,
		DemandSurge:              input.DemandSurge,
		WeatherSurge:             input.WeatherSurge,
		AverageSpeedKmh:          base.AverageSpeedKmh,
		DistanceCorrectionFactor: base.DistanceCorrectionFactor,
		CreatedBy:                staffID,
	})
}

// RetireRateCard takes a place's (or a class's) card away: trips there are
// priced with the next card. The previous versions stay on file.
func (s *Service) RetireRateCard(ctx context.Context, scope pricing.Scope, staffID string) error {
	scope, err := s.checkScope(ctx, scope)
	if err != nil {
		return err
	}

	if scope == (pricing.Scope{}) {
		return ErrCannotRetireBase
	}

	latest, found, err := s.repository.LatestRateCard(ctx, scope)
	if err != nil {
		return fmt.Errorf("read the rate card: %w", err)
	}

	if !found || latest.Retired {
		return ErrRateCardNotFound
	}

	latest.ID = ""
	latest.Retired = true
	latest.CreatedBy = staffID

	if _, err := s.repository.InsertRateCard(ctx, latest); err != nil {
		return fmt.Errorf("retire rate card: %w", err)
	}

	return nil
}

// --- surge rules -------------------------------------------------------------

func (s *Service) ListSurgeRules(ctx context.Context, filter Filter) ([]pricing.SurgeTimeRule, error) {
	filter, err := checkFilter(filter)
	if err != nil {
		return nil, err
	}

	return s.repository.ListSurgeRules(ctx, filter)
}

func (s *Service) CreateSurgeRule(ctx context.Context, input SurgeRuleInput) (pricing.SurgeTimeRule, error) {
	scope, err := s.checkScope(ctx, pricing.Scope{ZoneID: input.ZoneID, CityID: input.CityID})
	if err != nil {
		return pricing.SurgeTimeRule{}, err
	}

	rule, err := surgeRuleFrom(input)
	if err != nil {
		return pricing.SurgeTimeRule{}, err
	}

	rule.ZoneID, rule.CityID = scope.ZoneID, scope.CityID

	return s.repository.CreateSurgeRule(ctx, rule)
}

func (s *Service) UpdateSurgeRule(ctx context.Context, ruleID string, input SurgeRuleInput) (pricing.SurgeTimeRule, error) {
	ruleID = strings.TrimSpace(ruleID)
	if !looksLikeUUID(ruleID) {
		return pricing.SurgeTimeRule{}, ErrSurgeRuleNotFound
	}

	rule, err := surgeRuleFrom(input)
	if err != nil {
		return pricing.SurgeTimeRule{}, err
	}

	rule.ID = ruleID

	return s.repository.UpdateSurgeRule(ctx, rule)
}

func (s *Service) SetSurgeRuleActive(ctx context.Context, ruleID string, active bool) (pricing.SurgeTimeRule, error) {
	ruleID = strings.TrimSpace(ruleID)
	if !looksLikeUUID(ruleID) {
		return pricing.SurgeTimeRule{}, ErrSurgeRuleNotFound
	}

	return s.repository.SetSurgeRuleActive(ctx, ruleID, active)
}

func surgeRuleFrom(input SurgeRuleInput) (pricing.SurgeTimeRule, error) {
	label := strings.TrimSpace(input.Label)
	if label == "" || utf8.RuneCountInString(label) > maxLabelLength {
		return pricing.SurgeTimeRule{}, invalid("label", "must be 1 to 80 characters")
	}

	if input.DayOfWeek != nil && (*input.DayOfWeek < 0 || *input.DayOfWeek > 6) {
		return pricing.SurgeTimeRule{}, invalid("day_of_week", "must be 0 (Sunday) to 6 (Saturday), or unset for every day")
	}

	start, err := clockTime("start_time", input.StartTime)
	if err != nil {
		return pricing.SurgeTimeRule{}, err
	}

	end, err := clockTime("end_time", input.EndTime)
	if err != nil {
		return pricing.SurgeTimeRule{}, err
	}

	if start == end {
		return pricing.SurgeTimeRule{}, invalid("end_time", "must differ from start_time")
	}

	if err := checkPercent("surge_percent", input.SurgePercent, false); err != nil {
		return pricing.SurgeTimeRule{}, err
	}

	return pricing.SurgeTimeRule{
		Label:        label,
		DayOfWeek:    input.DayOfWeek,
		StartTime:    start,
		EndTime:      end,
		SurgePercent: input.SurgePercent,
		Active:       true,
	}, nil
}

// clockTime accepts "HH:MM" or "HH:MM:SS" and returns "HH:MM:SS".
func clockTime(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if !clockPattern.MatchString(value) {
		return "", invalid(field, `must be a time of day, "HH:MM"`)
	}

	if len(value) == len("15:04") {
		value += ":00"
	}

	return value, nil
}

// --- zone surges -------------------------------------------------------------

func (s *Service) ListZoneSurges(ctx context.Context, zoneID string, includePast bool) ([]pricing.ZoneSurge, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID != "" && !looksLikeUUID(zoneID) {
		return nil, ErrZoneNotFound
	}

	return s.repository.ListZoneSurges(ctx, zoneID, includePast, s.now())
}

func (s *Service) CreateZoneSurge(ctx context.Context, input ZoneSurgeInput, staffID string) (pricing.ZoneSurge, error) {
	zoneID := strings.TrimSpace(input.ZoneID)
	if zoneID == "" {
		return pricing.ZoneSurge{}, invalid("zone_id", "is required")
	}

	if _, err := s.checkScope(ctx, pricing.Scope{ZoneID: zoneID}); err != nil {
		return pricing.ZoneSurge{}, err
	}

	if err := checkPercent("surge_percent", input.SurgePercent, false); err != nil {
		return pricing.ZoneSurge{}, err
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxLabelLength {
		return pricing.ZoneSurge{}, invalid("reason", "must be 1 to 80 characters")
	}

	if input.DurationMinutes < 1 || input.DurationMinutes > maxZoneSurgeMinutes {
		return pricing.ZoneSurge{}, invalid("duration_minutes", "must be between 1 and 1440")
	}

	now := s.now()

	startsAt := input.StartsAt.UTC()
	if input.StartsAt.IsZero() {
		startsAt = now
	}

	if startsAt.Before(now.Add(-zoneSurgeStartTolerance)) || startsAt.After(now.Add(maxZoneSurgeLeadTime)) {
		return pricing.ZoneSurge{}, invalid("starts_at", "must be from now to 7 days ahead")
	}

	return s.repository.CreateZoneSurge(ctx, pricing.ZoneSurge{
		ZoneID:       zoneID,
		SurgePercent: input.SurgePercent,
		Reason:       reason,
		StartsAt:     startsAt,
		EndsAt:       startsAt.Add(time.Duration(input.DurationMinutes) * time.Minute),
		CreatedBy:    staffID,
	})
}

// EndZoneSurge ends a running surge now, or calls off one that has not
// started.
func (s *Service) EndZoneSurge(ctx context.Context, surgeID string) (pricing.ZoneSurge, error) {
	surgeID = strings.TrimSpace(surgeID)
	if !looksLikeUUID(surgeID) {
		return pricing.ZoneSurge{}, ErrZoneSurgeNotFound
	}

	return s.repository.EndZoneSurge(ctx, surgeID, s.now())
}

// --- checks ------------------------------------------------------------------

// checkScope trims and checks a place and class: at most one of zone and
// city, the one given an existing one, and the class a known one or empty
// (every class).
func (s *Service) checkScope(ctx context.Context, scope pricing.Scope) (pricing.Scope, error) {
	scope.ZoneID = strings.TrimSpace(scope.ZoneID)
	scope.CityID = strings.TrimSpace(scope.CityID)
	scope.VehicleClass = strings.ToLower(strings.TrimSpace(scope.VehicleClass))

	if scope.ZoneID != "" && scope.CityID != "" {
		return pricing.Scope{}, invalid("zone_id", "give a zone or a city, not both")
	}

	if scope.VehicleClass != "" {
		class, err := pricing.NormalizeVehicleClass(scope.VehicleClass)
		if err != nil {
			return pricing.Scope{}, invalid("vehicle_class", "must be economy, comfort, or empty for every class")
		}

		scope.VehicleClass = class
	}

	switch {
	case scope.ZoneID != "":
		if !looksLikeUUID(scope.ZoneID) {
			return pricing.Scope{}, ErrZoneNotFound
		}

		exists, err := s.places.ZoneExists(ctx, scope.ZoneID)
		if err != nil {
			return pricing.Scope{}, fmt.Errorf("%w: %v", ErrPlacesUnavailable, err)
		}

		if !exists {
			return pricing.Scope{}, ErrZoneNotFound
		}

	case scope.CityID != "":
		if !looksLikeUUID(scope.CityID) {
			return pricing.Scope{}, ErrCityNotFound
		}

		exists, err := s.places.CityExists(ctx, scope.CityID)
		if err != nil {
			return pricing.Scope{}, fmt.Errorf("%w: %v", ErrPlacesUnavailable, err)
		}

		if !exists {
			return pricing.Scope{}, ErrCityNotFound
		}
	}

	return scope, nil
}

func checkFilter(filter Filter) (Filter, error) {
	filter.CityID = strings.TrimSpace(filter.CityID)
	filter.ZoneID = strings.TrimSpace(filter.ZoneID)

	switch {
	case filter.CityID != "" && filter.ZoneID != "":
		return Filter{}, invalid("zone_id", "filter by a zone or a city, not both")
	case filter.CityID != "" && !looksLikeUUID(filter.CityID):
		return Filter{}, ErrCityNotFound
	case filter.ZoneID != "" && !looksLikeUUID(filter.ZoneID):
		return Filter{}, ErrZoneNotFound
	}

	return filter, nil
}

func checkMoney(field string, amount decimal.Decimal) error {
	switch {
	case amount.IsNegative():
		return invalid(field, "must not be negative")
	case amount.GreaterThan(maxMoney):
		return invalid(field, "is too large")
	case !amount.Equal(amount.Round(4)):
		return invalid(field, "has more than 4 decimal places")
	}

	return nil
}

// checkPercent accepts 0 (when zeroAllowed) up to 300, with at most two
// decimal places.
func checkPercent(field string, percent decimal.Decimal, zeroAllowed bool) error {
	switch {
	case percent.IsNegative(), !zeroAllowed && percent.IsZero():
		if zeroAllowed {
			return invalid(field, "must be between 0 and 300")
		}

		return invalid(field, "must be more than 0 and at most 300")
	case percent.GreaterThan(maxSurgePercent):
		return invalid(field, "must be at most 300")
	case !percent.Equal(percent.Round(2)):
		return invalid(field, "has more than 2 decimal places")
	}

	return nil
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}

	return true
}
