package postgres

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/tariffs"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	PRICING_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply every migration's Up section themselves (psql must be on PATH)
// and drop everything afterwards. Without the variable they are skipped.

const (
	testZone  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testCity  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testRider = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	testTrip  = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	otherTrip = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	otherZone = "ffffffff-ffff-4fff-8fff-ffffffffffff"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("PRICING_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PRICING_TEST_DATABASE_URL is not set")
	}

	files, err := filepath.Glob("../../../../migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}

	sort.Strings(files)

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	dropAll := func() {
		psql(`DROP TABLE IF EXISTS fare_quotes, zone_surges, fares, rider_trip_stats, coupon_redemptions,
		      coupons, surge_time_rules, pricing_configs, outbox_events, promotion_settings CASCADE;`)
	}

	dropAll()

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		up, _, _ := strings.Cut(string(raw), "-- +goose Down")
		psql(up)
	}

	t.Cleanup(dropAll)

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func card(scope pricing.Scope, base int64) pricing.Config {
	return pricing.Config{
		ZoneID: scope.ZoneID, CityID: scope.CityID, VehicleClass: scope.VehicleClass,
		CurrencyCode: "IQD", BaseFare: decimal.NewFromInt(base), PerKmRate: decimal.NewFromInt(500),
		PerMinuteRate: decimal.NewFromInt(50), MaxSurgePercent: decimal.NewFromInt(150),
		FreeWaitingMinutes: 3, CancellationGraceMinutes: 2,
		AverageSpeedKmh: 30, DistanceCorrectionFactor: 1.3,
	}
}

func TestTheMostSpecificCurrentCardPrices(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	base := func(scope pricing.Scope) int64 {
		t.Helper()

		got, err := repo.GetActiveConfig(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}

		return got.BaseFare.IntPart()
	}

	comfortInZone := pricing.Scope{ZoneID: testZone, CityID: testCity, VehicleClass: "comfort"}
	economyInZone := pricing.Scope{ZoneID: testZone, CityID: testCity, VehicleClass: "economy"}

	// Only the seeded card for everywhere.
	if got := base(comfortInZone); got != 2000 {
		t.Fatalf("seed: %d", got)
	}

	insert := func(c pricing.Config) {
		t.Helper()

		if _, err := repo.InsertRateCard(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	insert(card(pricing.Scope{VehicleClass: "comfort"}, 2500))
	insert(card(pricing.Scope{CityID: testCity}, 3000))
	insert(card(pricing.Scope{CityID: testCity, VehicleClass: "comfort"}, 3500))

	if got := base(comfortInZone); got != 3500 {
		t.Fatalf("city+class: %d", got)
	}

	if got := base(economyInZone); got != 3000 {
		t.Fatalf("city: %d", got)
	}

	insert(card(pricing.Scope{ZoneID: testZone}, 4000))

	if got := base(comfortInZone); got != 4000 {
		t.Fatalf("zone beats city: %d", got)
	}

	// A newer version wins; a retired newest version takes the zone out.
	insert(card(pricing.Scope{ZoneID: testZone}, 4200))

	if got := base(economyInZone); got != 4200 {
		t.Fatalf("newer: %d", got)
	}

	retired := card(pricing.Scope{ZoneID: testZone}, 4200)
	retired.Retired = true
	insert(retired)

	if got := base(comfortInZone); got != 3500 {
		t.Fatalf("after retiring the zone: %d", got)
	}

	if got := base(pricing.Scope{ZoneID: otherZone, VehicleClass: "comfort"}); got != 2500 {
		t.Fatalf("elsewhere, comfort: %d", got)
	}

	cards, err := repo.CurrentRateCards(ctx, tariffs.Filter{})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range cards {
		got = append(got, c.BaseFare.String())
	}

	// Everywhere (all classes, comfort), then the city's; the zone's is retired.
	if strings.Join(got, ",") != "2000,2500,3000,3500" {
		t.Fatalf("current cards %v", got)
	}

	cityCards, err := repo.CurrentRateCards(ctx, tariffs.Filter{CityID: testCity})
	if err != nil || len(cityCards) != 2 {
		t.Fatalf("city filter: %d %v", len(cityCards), err)
	}

	latest, found, err := repo.LatestRateCard(ctx, pricing.Scope{ZoneID: testZone})
	if err != nil || !found || !latest.Retired {
		t.Fatalf("latest zone version: %+v %v %v", latest, found, err)
	}

	// The base card cannot be retired, even directly in the table.
	baseRetired := card(pricing.Scope{}, 2000)
	baseRetired.Retired = true

	if _, err := repo.InsertRateCard(ctx, baseRetired); err == nil {
		t.Fatal("retired base card accepted")
	}
}

func quoteFor(rider string, expires time.Time) pricing.Quote {
	now := time.Now().UTC()

	return pricing.Quote{
		RiderID: rider, ZoneID: testZone, CityID: testCity, VehicleClass: "economy",
		Pickup: pricing.Point{Latitude: 36.19, Longitude: 44.01}, Dropoff: pricing.Point{Latitude: 36.2, Longitude: 44.02},
		Breakdown: pricing.FareBreakdown{
			CurrencyCode: "IQD", VehicleClass: "economy", ZoneID: testZone, Subtotal: decimal.NewFromInt(4000),
			Surge: pricing.SurgeBreakdown{TotalPercent: decimal.NewFromInt(15), Label: "Rush"}, Total: decimal.NewFromInt(4500),
		},
		DriversAvailable: true, PickupETAMinutes: 4, CreatedAt: now, ExpiresAt: expires,
	}
}

func TestQuotesAreClaimedOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)
	now := time.Now().UTC()

	saved, err := repo.SaveQuotes(ctx, []pricing.Quote{quoteFor(testRider, now.Add(5*time.Minute)), quoteFor(testRider, now.Add(time.Second))})
	if err != nil {
		t.Fatal(err)
	}

	live, short := saved[0], saved[1]
	if live.ID == "" || !live.Breakdown.Total.Equal(decimal.NewFromInt(4500)) || live.Breakdown.Surge.Label != "Rush" {
		t.Fatalf("saved %+v", live)
	}

	if _, err := repo.ClaimQuote(ctx, live.ID, otherTrip[:35]+"0", testTrip, now); !errors.Is(err, pricing.ErrQuoteNotFound) {
		t.Fatalf("another rider: %v", err)
	}

	claimed, err := repo.ClaimQuote(ctx, live.ID, testRider, testTrip, now)
	if err != nil || claimed.ClaimedTripID != testTrip {
		t.Fatalf("claim: %+v %v", claimed, err)
	}

	if again, err := repo.ClaimQuote(ctx, live.ID, testRider, testTrip, now); err != nil || again.ID != live.ID {
		t.Fatalf("again: %v", err)
	}

	if _, err := repo.ClaimQuote(ctx, live.ID, testRider, otherTrip, now); !errors.Is(err, pricing.ErrQuoteAlreadyUsed) {
		t.Fatalf("another trip: %v", err)
	}

	if _, err := repo.ClaimQuote(ctx, short.ID, testRider, otherTrip, now.Add(time.Minute)); !errors.Is(err, pricing.ErrQuoteExpired) {
		t.Fatalf("expired: %v", err)
	}

	if _, err := repo.ClaimQuote(ctx, short.ID, testRider, testTrip, now); !errors.Is(err, pricing.ErrQuoteNotForTrip) {
		t.Fatalf("a trip with two quotes: %v", err)
	}

	if err := repo.ReleaseQuote(ctx, live.ID, otherTrip); err != nil {
		t.Fatal(err)
	}

	if found, _ := repo.FindQuote(ctx, live.ID); found.ClaimedTripID != testTrip {
		t.Fatal("a release by another trip must change nothing")
	}

	if err := repo.ReleaseQuote(ctx, live.ID, testTrip); err != nil {
		t.Fatal(err)
	}

	if found, _ := repo.FindQuote(ctx, live.ID); found.ClaimedTripID != "" {
		t.Fatal("released")
	}

	removed, err := repo.DeleteUnclaimedQuotes(ctx, now.Add(10*time.Minute))
	if err != nil || removed != 2 {
		t.Fatalf("cleanup removed %d, %v", removed, err)
	}
}

func TestDemandCountsOtherRidersInTheZone(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)
	now := time.Now().UTC()
	other := "99999999-9999-4999-8999-999999999999"

	for _, rider := range []string{testRider, testRider, other, other} {
		if _, err := repo.SaveQuotes(ctx, []pricing.Quote{quoteFor(rider, now.Add(time.Minute))}); err != nil {
			t.Fatal(err)
		}
	}

	count, err := repo.CountQuotingRiders(ctx, testZone, testRider, now.Add(-time.Minute))
	if err != nil || count != 1 {
		t.Fatalf("count %d %v", count, err)
	}

	if count, _ := repo.CountQuotingRiders(ctx, otherZone, testRider, now.Add(-time.Minute)); count != 0 {
		t.Fatalf("other zone %d", count)
	}
}

func TestZoneSurgesRunForTheirWindow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)
	now := time.Now().UTC()

	create := func(percent int64, start, end time.Time) pricing.ZoneSurge {
		t.Helper()

		surge, err := repo.CreateZoneSurge(ctx, pricing.ZoneSurge{
			ZoneID: testZone, SurgePercent: decimal.NewFromInt(percent), Reason: "Concert", StartsAt: start, EndsAt: end,
		})
		if err != nil {
			t.Fatal(err)
		}

		return surge
	}

	running := create(30, now.Add(-time.Minute), now.Add(time.Hour))
	create(60, now.Add(time.Hour), now.Add(2*time.Hour))

	got, found, err := repo.ActiveZoneSurge(ctx, testZone, now)
	if err != nil || !found || got.ID != running.ID {
		t.Fatalf("active: %+v %v %v", got, found, err)
	}

	if _, err := repo.EndZoneSurge(ctx, running.ID, now); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := repo.ActiveZoneSurge(ctx, testZone, now); found {
		t.Fatal("ended")
	}

	if _, err := repo.EndZoneSurge(ctx, running.ID, now); !errors.Is(err, tariffs.ErrZoneSurgeOver) {
		t.Fatalf("twice: %v", err)
	}

	if _, err := repo.EndZoneSurge(ctx, testTrip, now); !errors.Is(err, tariffs.ErrZoneSurgeNotFound) {
		t.Fatalf("unknown: %v", err)
	}

	upcoming, err := repo.ListZoneSurges(ctx, testZone, false, now)
	if err != nil || len(upcoming) != 1 {
		t.Fatalf("upcoming %d %v", len(upcoming), err)
	}

	all, err := repo.ListZoneSurges(ctx, "", true, now)
	if err != nil || len(all) != 2 {
		t.Fatalf("all %d %v", len(all), err)
	}
}

func TestSurgeRulesWithAPlace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)
	friday := 5

	rule, err := repo.CreateSurgeRule(ctx, pricing.SurgeTimeRule{
		Label: "Friday", CityID: testCity, DayOfWeek: &friday, StartTime: "11:30:00", EndTime: "13:00:00",
		SurgePercent: decimal.NewFromInt(20),
	})
	if err != nil || rule.CityID != testCity || *rule.DayOfWeek != 5 || !rule.Active {
		t.Fatalf("created %+v %v", rule, err)
	}

	rule.StartTime, rule.DayOfWeek = "12:00:00", nil
	if updated, err := repo.UpdateSurgeRule(ctx, rule); err != nil || updated.StartTime != "12:00:00" || updated.DayOfWeek != nil {
		t.Fatalf("updated %+v %v", updated, err)
	}

	if _, err := repo.SetSurgeRuleActive(ctx, rule.ID, false); err != nil {
		t.Fatal(err)
	}

	active, err := repo.ListActiveSurgeTimeRules(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range active {
		if r.ID == rule.ID {
			t.Fatal("inactive rule listed as active")
		}
	}

	cityRules, err := repo.ListSurgeRules(ctx, tariffs.Filter{CityID: testCity})
	if err != nil || len(cityRules) != 1 || cityRules[0].Active {
		t.Fatalf("city rules %+v %v", cityRules, err)
	}

	if _, err := repo.SetSurgeRuleActive(ctx, testTrip, true); !errors.Is(err, tariffs.ErrSurgeRuleNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestAFareKeepsWhatItWasPricedWith(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	saved, err := repo.SaveQuotes(ctx, []pricing.Quote{quoteFor(testRider, time.Now().UTC().Add(time.Minute))})
	if err != nil {
		t.Fatal(err)
	}

	breakdown := saved[0].Breakdown
	breakdown.CityID = testCity
	breakdown.MinimumFareAdjustment = decimal.NewFromInt(250)
	breakdown.Surge.ZonePercent = decimal.NewFromInt(15)

	input := pricing.PersistFareInput{TripID: testTrip, RiderID: testRider, Breakdown: breakdown, QuoteID: saved[0].ID}

	fare, err := repo.PersistFare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	if fare.QuoteID != saved[0].ID || fare.Breakdown.CityID != testCity || fare.Breakdown.Surge.Label != "Rush" ||
		!fare.Breakdown.MinimumFareAdjustment.Equal(decimal.NewFromInt(250)) || !fare.Breakdown.Total.Equal(decimal.NewFromInt(4500)) {
		t.Fatalf("fare %+v", fare)
	}

	if _, err := repo.PersistFare(ctx, input); !errors.Is(err, pricing.ErrFareAlreadyRecorded) {
		t.Fatalf("twice: %v", err)
	}

	var payload string
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM outbox_events WHERE event_type = 'fare.calculated'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(payload, saved[0].ID) {
		t.Fatalf("payload %s", payload)
	}
}
