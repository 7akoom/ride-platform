package incentives

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func d(s string) wallet.Money { return decimal.RequireFromString(s) }

type fakeStore struct {
	Store
	due      *Campaign
	byKey    map[string]Campaign
	created  []Campaign
	settled  []Payout
	marked   []string
	visible  []Campaign
	payouts  map[string]Payout
	claimArg time.Time
}

func (f *fakeStore) Config(context.Context) (wallet.Config, error) {
	return wallet.Config{CurrencyCode: "IQD"}, nil
}

func (f *fakeStore) FindByKey(_ context.Context, key string) (Campaign, bool, error) {
	c, ok := f.byKey[key]

	return c, ok, nil
}

func (f *fakeStore) Create(_ context.Context, c Campaign, _, _ string) (Campaign, error) {
	c.ID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	f.created = append(f.created, c)

	return c, nil
}

func (f *fakeStore) ClaimDue(_ context.Context, endedBefore time.Time, _ time.Duration) (Campaign, bool, error) {
	f.claimArg = endedBefore

	if f.due == nil || f.due.EndsAt.After(endedBefore) {
		return Campaign{}, false, nil
	}

	c := *f.due
	f.due = nil

	return c, true, nil
}

func (f *fakeStore) Settle(_ context.Context, _ Campaign, p Payout) (Payout, error) {
	f.settled = append(f.settled, p)

	return p, nil
}

func (f *fakeStore) MarkSettled(_ context.Context, id string, _ time.Time) error {
	f.marked = append(f.marked, id)

	return nil
}

func (f *fakeStore) Visible(context.Context, time.Time) ([]Campaign, error) { return f.visible, nil }

func (f *fakeStore) DriverPayouts(context.Context, string, []string) (map[string]Payout, error) {
	return f.payouts, nil
}

type fakeTrips struct {
	pages  [][]Activity
	driver Activity
	min    int
}

func (f *fakeTrips) Driver(context.Context, string, Campaign) (Activity, error) { return f.driver, nil }

func (f *fakeTrips) Drivers(_ context.Context, _ Campaign, minCompleted int, token string) ([]Activity, string, error) {
	f.min = minCompleted
	i := 0

	if token != "" {
		i = int(token[0] - '0')
	}

	next := ""
	if i+1 < len(f.pages) {
		next = string(rune('0' + i + 1))
	}

	return f.pages[i], next, nil
}

type fakeDrivers struct {
	profiles map[string]Profile
}

func (f *fakeDrivers) Profile(_ context.Context, id string) (Profile, error) {
	p, ok := f.profiles[id]
	if !ok {
		return Profile{}, ErrDriverNotFound
	}

	return p, nil
}

var now = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func newService(store *fakeStore, trips *fakeTrips, drivers *fakeDrivers) *Service {
	s := NewService(store, trips, drivers, Settings{TimeZone: "Asia/Baghdad", SettleDelay: 30 * time.Minute},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }

	return s
}

func validInput() CreateInput {
	return CreateInput{
		Name: " Night quest ", StartsAt: now.Add(time.Hour), EndsAt: now.Add(7 * 24 * time.Hour),
		ZoneIDs:      []string{"11111111-1111-4111-8111-111111111111", "11111111-1111-4111-8111-111111111111"},
		VehicleClass: "Economy", DailyStart: "22:00", DailyEnd: "02:00",
		MinAcceptanceRate: "80", MaxCancellationRate: "10.5", MinRating: "4.5",
		Tiers: []Tier{{Trips: 10, Amount: d("10000")}, {Trips: 25, Amount: d("30000")}},
	}
}

func TestCreateChecksAndNormalises(t *testing.T) {
	store := &fakeStore{}
	s := newService(store, &fakeTrips{}, &fakeDrivers{})

	c, err := s.Create(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}

	if c.Name != "Night quest" || c.VehicleClass != "economy" || len(c.ZoneIDs) != 1 || c.DailyStart != 1320 ||
		c.DailyEnd != 120 || c.TimeZone != "Asia/Baghdad" || c.CurrencyCode != "IQD" || c.MinRating == nil ||
		!c.MaxCancellationRate.Equal(d("10.5")) {
		t.Fatalf("campaign: %+v", c)
	}
}

func TestCreateRefusesBadInput(t *testing.T) {
	cases := map[string]struct {
		change func(*CreateInput)
		want   error
	}{
		"no name":         {func(in *CreateInput) { in.Name = " " }, ErrInvalidName},
		"ends first":      {func(in *CreateInput) { in.EndsAt = in.StartsAt }, ErrInvalidPeriod},
		"too long":        {func(in *CreateInput) { in.EndsAt = in.StartsAt.Add(32 * 24 * time.Hour) }, ErrInvalidPeriod},
		"in the past":     {func(in *CreateInput) { in.StartsAt = now.Add(-time.Hour) }, ErrInvalidPeriod},
		"bad zone":        {func(in *CreateInput) { in.ZoneIDs = []string{"x"} }, ErrInvalidScope},
		"bad class":       {func(in *CreateInput) { in.VehicleClass = "luxury" }, ErrInvalidScope},
		"half hours":      {func(in *CreateInput) { in.DailyEnd = "" }, ErrInvalidHours},
		"bad clock":       {func(in *CreateInput) { in.DailyStart = "24:00" }, ErrInvalidHours},
		"bad zone name":   {func(in *CreateInput) { in.TimeZone = "Mars/Base" }, ErrInvalidTimeZone},
		"rate over 100":   {func(in *CreateInput) { in.MinAcceptanceRate = "101" }, ErrInvalidConditions},
		"rating over 5":   {func(in *CreateInput) { in.MinRating = "5.1" }, ErrInvalidConditions},
		"no tiers":        {func(in *CreateInput) { in.Tiers = nil }, ErrInvalidTiers},
		"six tiers":       {func(in *CreateInput) { in.Tiers = make([]Tier, 6) }, ErrInvalidTiers},
		"amount shrinks":  {func(in *CreateInput) { in.Tiers[1].Amount = d("5000") }, ErrInvalidTiers},
		"trips repeat":    {func(in *CreateInput) { in.Tiers[1].Trips = 10 }, ErrInvalidTiers},
		"four decimals":   {func(in *CreateInput) { in.Tiers[0].Amount = d("1.0001") }, ErrInvalidTiers},
		"long idem. key":  {func(in *CreateInput) { in.IdempotencyKey = string(make([]byte, 121)) }, ErrIdempotencyKey},
		"zero tier trips": {func(in *CreateInput) { in.Tiers[0].Trips = 0 }, ErrInvalidTiers},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			tc.change(&in)

			if _, err := newService(&fakeStore{}, &fakeTrips{}, &fakeDrivers{}).Create(context.Background(), in); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCreateWithAUsedKey(t *testing.T) {
	in := validInput()
	in.IdempotencyKey = "k"

	earlier := Campaign{ID: "e", Name: "Night quest", StartsAt: in.StartsAt, EndsAt: in.EndsAt}
	store := &fakeStore{byKey: map[string]Campaign{"k": earlier}}
	s := newService(store, &fakeTrips{}, &fakeDrivers{})

	if c, err := s.Create(context.Background(), in); err != nil || c.ID != "e" || len(store.created) != 0 {
		t.Fatalf("a retry should return the earlier campaign: %+v %v", c, err)
	}

	in.Name = "Other"
	if _, err := s.Create(context.Background(), in); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("got %v", err)
	}
}

func TestRatesAndDecide(t *testing.T) {
	acceptance, cancellation := Rates(Activity{})
	if !acceptance.Equal(d("100")) || !cancellation.IsZero() {
		t.Fatalf("no offers: %s %s", acceptance, cancellation)
	}

	acceptance, cancellation = Rates(Activity{CompletedTrips: 9, DriverCancellations: 1, OffersAccepted: 2, OffersDeclined: 1})
	if !acceptance.Equal(d("66.67")) || !cancellation.Equal(d("10")) {
		t.Fatalf("rates: %s %s", acceptance, cancellation)
	}

	min, rating := d("80"), d("4.5")
	c := Campaign{ID: "c", MinAcceptanceRate: &min, MinRating: &rating,
		Tiers: []Tier{{Trips: 10, Amount: d("10000")}, {Trips: 25, Amount: d("30000")}}}

	if p := c.Decide(Activity{CompletedTrips: 9}, nil); p.Status != PayoutNotEligible || p.TierTrips != 0 || len(p.Unmet) != 0 {
		t.Fatalf("below the first tier: %+v", p)
	}

	if p := c.Decide(Activity{CompletedTrips: 30}, nil); p.Status != PayoutPaid || !p.Amount.Equal(d("30000")) || p.TierTrips != 25 {
		t.Fatalf("an unrated driver reaching the top: %+v", p)
	}

	low := d("4.2")
	p := c.Decide(Activity{CompletedTrips: 12, OffersAccepted: 1, OffersDeclined: 1}, &low)

	if p.Status != PayoutNotEligible || !p.Amount.IsZero() || p.TierTrips != 10 || len(p.Unmet) != 2 ||
		p.Unmet[0] != UnmetAcceptance || p.Unmet[1] != UnmetRating {
		t.Fatalf("conditions unmet: %+v", p)
	}

	if reached, next := c.Reached(10); reached.Trips != 10 || next.Trips != 25 {
		t.Fatalf("reached: %v %v", reached, next)
	}
}

func TestStateAt(t *testing.T) {
	c := Campaign{Status: StatusActive, StartsAt: now, EndsAt: now.Add(time.Hour)}

	for at, want := range map[time.Time]State{
		now.Add(-time.Minute): StateScheduled,
		now:                   StateRunning,
		now.Add(time.Hour):    StateSettling,
	} {
		if got := c.StateAt(at); got != want {
			t.Fatalf("at %v: %s, want %s", at, got, want)
		}
	}

	c.Status = StatusCancelled
	if c.StateAt(now) != StateCancelled {
		t.Fatal("cancelled")
	}
}

func TestSettleDuePaysEveryPageAfterTheDelay(t *testing.T) {
	rating := d("4.5")
	due := Campaign{ID: "c1", EndsAt: now.Add(-time.Hour), MinRating: &rating,
		Tiers: []Tier{{Trips: 5, Amount: d("5000")}, {Trips: 10, Amount: d("12000")}}}

	low, good := d("4.0"), d("4.8")
	store := &fakeStore{due: &due}
	trips := &fakeTrips{pages: [][]Activity{
		{{DriverID: "a", CompletedTrips: 11}, {DriverID: "b", CompletedTrips: 6}},
		{{DriverID: "c", CompletedTrips: 7}},
	}}
	drivers := &fakeDrivers{profiles: map[string]Profile{"a": {Rating: &good}, "b": {Rating: &low}, "c": {}}}

	s := newService(store, trips, drivers)

	done, err := s.SettleDue(context.Background(), time.Minute)
	if err != nil || !done {
		t.Fatalf("settle: %v %v", done, err)
	}

	if !store.claimArg.Equal(now.Add(-30*time.Minute)) || trips.min != 5 {
		t.Fatalf("claimed before %v with min %d", store.claimArg, trips.min)
	}

	if len(store.settled) != 3 || len(store.marked) != 1 {
		t.Fatalf("settled %d, marked %d", len(store.settled), len(store.marked))
	}

	a, b, c := store.settled[0], store.settled[1], store.settled[2]
	if a.Status != PayoutPaid || !a.Amount.Equal(d("12000")) || b.Status != PayoutNotEligible || b.Unmet[0] != UnmetRating ||
		c.Status != PayoutPaid || !c.Amount.Equal(d("5000")) {
		t.Fatalf("payouts: %+v", store.settled)
	}

	if done, err := s.SettleDue(context.Background(), time.Minute); done || err != nil {
		t.Fatalf("nothing more is due: %v %v", done, err)
	}
}

func TestSettleDueWaitsForTheDelay(t *testing.T) {
	due := Campaign{ID: "c1", EndsAt: now.Add(-10 * time.Minute), Tiers: []Tier{{Trips: 1, Amount: d("1")}}}
	store := &fakeStore{due: &due}

	if done, err := newService(store, &fakeTrips{}, &fakeDrivers{}).SettleDue(context.Background(), time.Minute); done || err != nil {
		t.Fatalf("a campaign ended 10 minutes ago is not due yet: %v %v", done, err)
	}
}

func TestForDriverShowsProgressForTheirClass(t *testing.T) {
	min := d("90")
	running := Campaign{ID: "r", Status: StatusActive, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
		MinAcceptanceRate: &min, Tiers: []Tier{{Trips: 5, Amount: d("5000")}, {Trips: 10, Amount: d("9000")}}}
	scheduled := Campaign{ID: "s", Status: StatusActive, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour),
		Tiers: []Tier{{Trips: 3, Amount: d("1000")}}}
	comfort := Campaign{ID: "x", Status: StatusActive, VehicleClass: "comfort", StartsAt: now, EndsAt: now.Add(time.Hour),
		Tiers: []Tier{{Trips: 3, Amount: d("1000")}}}

	store := &fakeStore{visible: []Campaign{running, scheduled, comfort}, payouts: map[string]Payout{}}
	trips := &fakeTrips{driver: Activity{CompletedTrips: 6, OffersAccepted: 8, OffersDeclined: 2}}
	drivers := &fakeDrivers{profiles: map[string]Profile{"dddddddd-dddd-4ddd-8ddd-dddddddddddd": {VehicleClass: "economy"}}}

	views, err := newService(store, trips, drivers).ForDriver(context.Background(), "DDDDDDDD-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}

	if len(views) != 2 {
		t.Fatalf("the comfort campaign should be hidden: %d", len(views))
	}

	r := views[0]
	if r.CompletedTrips != 6 || r.Reached.Trips != 5 || r.Next.Trips != 10 || !r.AcceptanceRate.Equal(d("80")) ||
		len(r.Unmet) != 1 || r.Unmet[0] != UnmetAcceptance {
		t.Fatalf("running view: %+v", r)
	}

	if s := views[1]; s.CompletedTrips != 0 || s.Reached != nil || s.Next.Trips != 3 {
		t.Fatalf("scheduled view: %+v", s)
	}

	if _, err := newService(store, trips, drivers).ForDriver(context.Background(), "nope"); !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("got %v", err)
	}
}
