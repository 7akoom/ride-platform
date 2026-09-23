package tariffs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const (
	cityID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	zoneID  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	staffID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	otherID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
)

type fakePlaces struct {
	err error
}

func (p fakePlaces) CityExists(_ context.Context, id string) (bool, error) {
	return id == cityID, p.err
}
func (p fakePlaces) ZoneExists(_ context.Context, id string) (bool, error) {
	return id == zoneID, p.err
}

type fakeRepository struct {
	cards   []pricing.Config
	rules   []pricing.SurgeTimeRule
	surges  []pricing.ZoneSurge
	updated pricing.SurgeTimeRule
}

func (r *fakeRepository) CurrentRateCards(context.Context, Filter) ([]pricing.Config, error) {
	return r.cards, nil
}

func (r *fakeRepository) LatestRateCard(_ context.Context, scope pricing.Scope) (pricing.Config, bool, error) {
	for i := len(r.cards) - 1; i >= 0; i-- {
		c := r.cards[i]
		if c.ZoneID == scope.ZoneID && c.CityID == scope.CityID && c.VehicleClass == scope.VehicleClass {
			return c, true, nil
		}
	}

	return pricing.Config{}, false, nil
}

func (r *fakeRepository) InsertRateCard(_ context.Context, card pricing.Config) (pricing.Config, error) {
	card.ID = "new"
	r.cards = append(r.cards, card)

	return card, nil
}

func (r *fakeRepository) ListSurgeRules(context.Context, Filter) ([]pricing.SurgeTimeRule, error) {
	return r.rules, nil
}

func (r *fakeRepository) CreateSurgeRule(_ context.Context, rule pricing.SurgeTimeRule) (pricing.SurgeTimeRule, error) {
	r.rules = append(r.rules, rule)

	return rule, nil
}

func (r *fakeRepository) UpdateSurgeRule(_ context.Context, rule pricing.SurgeTimeRule) (pricing.SurgeTimeRule, error) {
	r.updated = rule

	return rule, nil
}

func (r *fakeRepository) SetSurgeRuleActive(context.Context, string, bool) (pricing.SurgeTimeRule, error) {
	return pricing.SurgeTimeRule{}, nil
}

func (r *fakeRepository) ListZoneSurges(context.Context, string, bool, time.Time) ([]pricing.ZoneSurge, error) {
	return r.surges, nil
}

func (r *fakeRepository) CreateZoneSurge(_ context.Context, surge pricing.ZoneSurge) (pricing.ZoneSurge, error) {
	r.surges = append(r.surges, surge)

	return surge, nil
}

func (r *fakeRepository) FindZoneSurge(context.Context, string) (pricing.ZoneSurge, error) {
	return pricing.ZoneSurge{}, ErrZoneSurgeNotFound
}

func (r *fakeRepository) EndZoneSurge(context.Context, string, time.Time) (pricing.ZoneSurge, error) {
	return pricing.ZoneSurge{}, ErrZoneSurgeOver
}

func rig() (*Service, *fakeRepository) {
	repo := &fakeRepository{cards: []pricing.Config{{
		ID: "base", CurrencyCode: "IQD", BaseFare: decimal.NewFromInt(2000),
		AverageSpeedKmh: 30, DistanceCorrectionFactor: 1.3,
	}}}

	svc := NewService(repo, fakePlaces{})
	svc.now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }

	return svc, repo
}

func money(v int64) decimal.Decimal { return decimal.NewFromInt(v) }

func cardInput() RateCardInput {
	return RateCardInput{
		Scope:    pricing.Scope{CityID: cityID, VehicleClass: " Comfort "},
		BaseFare: money(3000), PerKmRate: money(600), PerMinuteRate: money(60),
		MinimumFare: money(4000), FreeWaitingMinutes: 3, WaitingPerMinute: money(100),
		CancellationFee: money(1000), CancellationGraceMinutes: 2, NoShowFee: money(2000),
		DemandSurge: true,
	}
}

func TestSettingARateCard(t *testing.T) {
	svc, repo := rig()

	card, err := svc.SetRateCard(context.Background(), cardInput(), staffID)
	if err != nil {
		t.Fatal(err)
	}

	if card.CityID != cityID || card.VehicleClass != "comfort" || card.CurrencyCode != "IQD" ||
		card.AverageSpeedKmh != 30 || card.DistanceCorrectionFactor != 1.3 || card.CreatedBy != staffID {
		t.Fatalf("card %+v", card)
	}

	if !card.MaxSurgePercent.Equal(money(150)) || !card.DemandSurge || card.WeatherSurge {
		t.Fatalf("surge settings %+v", card)
	}

	if len(repo.cards) != 2 {
		t.Fatal("a new version is added")
	}
}

func TestRateCardRefusals(t *testing.T) {
	cases := map[string]struct {
		mutate func(*RateCardInput)
		want   error
		field  string
	}{
		"zone and city":     {func(i *RateCardInput) { i.Scope.ZoneID = zoneID }, nil, "zone_id"},
		"unknown city":      {func(i *RateCardInput) { i.Scope.CityID = otherID }, ErrCityNotFound, ""},
		"unknown zone":      {func(i *RateCardInput) { i.Scope = pricing.Scope{ZoneID: otherID} }, ErrZoneNotFound, ""},
		"malformed city":    {func(i *RateCardInput) { i.Scope.CityID = "erbil" }, ErrCityNotFound, ""},
		"unknown class":     {func(i *RateCardInput) { i.Scope.VehicleClass = "van" }, nil, "vehicle_class"},
		"negative fee":      {func(i *RateCardInput) { i.CancellationFee = money(-1) }, nil, "cancellation_fee"},
		"too many decimals": {func(i *RateCardInput) { i.PerKmRate = decimal.RequireFromString("1.23456") }, nil, "per_km_rate"},
		"too large":         {func(i *RateCardInput) { i.BaseFare = money(100_000_000) }, nil, "base_fare"},
		"waiting minutes":   {func(i *RateCardInput) { i.FreeWaitingMinutes = 61 }, nil, "free_waiting_minutes"},
		"grace minutes":     {func(i *RateCardInput) { i.CancellationGraceMinutes = -1 }, nil, "cancellation_grace_minutes"},
		"surge over 300":    {func(i *RateCardInput) { v := money(301); i.MaxSurgePercent = &v }, nil, "max_surge_percent"},
		"surge 3 decimals":  {func(i *RateCardInput) { v := decimal.RequireFromString("1.005"); i.MaxSurgePercent = &v }, nil, "max_surge_percent"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo := rig()
			input := cardInput()
			tc.mutate(&input)

			_, err := svc.SetRateCard(context.Background(), input, staffID)

			var invalidErr *InvalidError
			switch {
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("got %v, want %v", err, tc.want)
			case tc.field != "" && (!errors.As(err, &invalidErr) || invalidErr.Field != tc.field):
				t.Fatalf("got %v, want a problem with %s", err, tc.field)
			}

			if len(repo.cards) != 1 {
				t.Fatal("nothing may be added")
			}
		})
	}
}

func TestASurgeOfZeroIsAllowedOnARateCard(t *testing.T) {
	svc, _ := rig()
	input := cardInput()
	zero := decimal.Zero
	input.MaxSurgePercent = &zero

	card, err := svc.SetRateCard(context.Background(), input, staffID)
	if err != nil || !card.MaxSurgePercent.IsZero() {
		t.Fatalf("%+v %v", card, err)
	}
}

func TestPlacesThatCannotBeCheckedFailClosed(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakePlaces{err: errors.New("location-service down")})

	if _, err := svc.SetRateCard(context.Background(), cardInput(), staffID); !errors.Is(err, ErrPlacesUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestRetiringARateCard(t *testing.T) {
	svc, repo := rig()

	if err := svc.RetireRateCard(context.Background(), pricing.Scope{}, staffID); !errors.Is(err, ErrCannotRetireBase) {
		t.Fatalf("base: %v", err)
	}

	scope := pricing.Scope{CityID: cityID}
	if err := svc.RetireRateCard(context.Background(), scope, staffID); !errors.Is(err, ErrRateCardNotFound) {
		t.Fatalf("none there: %v", err)
	}

	input := cardInput()
	input.Scope = scope

	if _, err := svc.SetRateCard(context.Background(), input, staffID); err != nil {
		t.Fatal(err)
	}

	if err := svc.RetireRateCard(context.Background(), scope, otherID); err != nil {
		t.Fatal(err)
	}

	retired := repo.cards[len(repo.cards)-1]
	if !retired.Retired || retired.CityID != cityID || retired.CreatedBy != otherID || !retired.BaseFare.Equal(money(3000)) {
		t.Fatalf("retired %+v", retired)
	}

	if err := svc.RetireRateCard(context.Background(), scope, staffID); !errors.Is(err, ErrRateCardNotFound) {
		t.Fatalf("twice: %v", err)
	}
}

func TestSurgeRules(t *testing.T) {
	svc, repo := rig()
	friday := 5

	rule, err := svc.CreateSurgeRule(context.Background(), SurgeRuleInput{
		ZoneID: zoneID, Label: " Friday prayers ", DayOfWeek: &friday,
		StartTime: "11:30", EndTime: "13:00:00", SurgePercent: money(20),
	})
	if err != nil {
		t.Fatal(err)
	}

	if rule.Label != "Friday prayers" || rule.StartTime != "11:30:00" || rule.EndTime != "13:00:00" ||
		rule.ZoneID != zoneID || !rule.Active || len(repo.rules) != 1 {
		t.Fatalf("rule %+v", rule)
	}

	bad := []SurgeRuleInput{
		{Label: "", StartTime: "07:00", EndTime: "09:00", SurgePercent: money(10)},
		{Label: "x", StartTime: "7:00", EndTime: "09:00", SurgePercent: money(10)},
		{Label: "x", StartTime: "24:00", EndTime: "09:00", SurgePercent: money(10)},
		{Label: "x", StartTime: "09:00", EndTime: "09:00:00", SurgePercent: money(10)},
		{Label: "x", StartTime: "07:00", EndTime: "09:00", SurgePercent: decimal.Zero},
		{Label: "x", StartTime: "07:00", EndTime: "09:00", SurgePercent: money(301)},
		{Label: "x", DayOfWeek: new(int), StartTime: "07:00", EndTime: "09:00", SurgePercent: money(10), CityID: cityID, ZoneID: zoneID},
	}

	seven := 7
	bad = append(bad, SurgeRuleInput{Label: "x", DayOfWeek: &seven, StartTime: "07:00", EndTime: "09:00", SurgePercent: money(10)})

	for i, input := range bad {
		var invalidErr *InvalidError
		if _, err := svc.CreateSurgeRule(context.Background(), input); !errors.As(err, &invalidErr) {
			t.Fatalf("case %d: got %v", i, err)
		}
	}

	if _, err := svc.UpdateSurgeRule(context.Background(), "rule-1", SurgeRuleInput{Label: "x", StartTime: "07:00", EndTime: "09:00", SurgePercent: money(10)}); !errors.Is(err, ErrSurgeRuleNotFound) {
		t.Fatalf("malformed id: %v", err)
	}

	if _, err := svc.UpdateSurgeRule(context.Background(), otherID, SurgeRuleInput{Label: "Late", StartTime: "23:00", EndTime: "04:00", SurgePercent: money(15)}); err != nil {
		t.Fatal(err)
	}

	if repo.updated.ID != otherID || repo.updated.StartTime != "23:00:00" {
		t.Fatalf("updated %+v", repo.updated)
	}
}

func TestZoneSurges(t *testing.T) {
	svc, repo := rig()

	surge, err := svc.CreateZoneSurge(context.Background(), ZoneSurgeInput{
		ZoneID: zoneID, SurgePercent: money(40), Reason: " Concert ", DurationMinutes: 90,
	}, staffID)
	if err != nil {
		t.Fatal(err)
	}

	start := svc.now()
	if surge.Reason != "Concert" || !surge.StartsAt.Equal(start) || !surge.EndsAt.Equal(start.Add(90*time.Minute)) ||
		surge.CreatedBy != staffID || len(repo.surges) != 1 {
		t.Fatalf("surge %+v", surge)
	}

	bad := map[string]ZoneSurgeInput{
		"no zone":       {SurgePercent: money(40), Reason: "x", DurationMinutes: 10},
		"unknown zone":  {ZoneID: otherID, SurgePercent: money(40), Reason: "x", DurationMinutes: 10},
		"no reason":     {ZoneID: zoneID, SurgePercent: money(40), Reason: " ", DurationMinutes: 10},
		"too long":      {ZoneID: zoneID, SurgePercent: money(40), Reason: "x", DurationMinutes: 1441},
		"in the past":   {ZoneID: zoneID, SurgePercent: money(40), Reason: "x", DurationMinutes: 10, StartsAt: start.Add(-time.Hour)},
		"too far ahead": {ZoneID: zoneID, SurgePercent: money(40), Reason: "x", DurationMinutes: 10, StartsAt: start.Add(8 * 24 * time.Hour)},
		"zero percent":  {ZoneID: zoneID, Reason: "x", DurationMinutes: 10},
	}

	for name, input := range bad {
		if _, err := svc.CreateZoneSurge(context.Background(), input, staffID); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}

	if _, err := svc.EndZoneSurge(context.Background(), "surge-1"); !errors.Is(err, ErrZoneSurgeNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
}
