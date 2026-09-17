package pricing

// Internal test package (not _test) so it can exercise the unexported
// haversineDistanceKm, fallbackRoute, timeInWindow, timeOfDaySurgePercent.

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// --- haversineDistanceKm / fallbackRoute -----------------------------------

func TestHaversineDistanceKm(t *testing.T) {
	cases := []struct {
		name                    string
		lat1, lng1, lat2, lng2 float64
		want                   float64
		tolerance              float64
	}{
		{"same point", 36.19, 44.01, 36.19, 44.01, 0, 0.001},
		{"antipodal points, half the great circle", 0, 0, 0, 180, math.Pi * earthRadiusKm, 0.1},
		{"quarter of the great circle", 0, 0, 0, 90, (math.Pi / 2) * earthRadiusKm, 0.1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := haversineDistanceKm(tc.lat1, tc.lng1, tc.lat2, tc.lng2)
			if !almostEqual(got, tc.want, tc.tolerance) {
				t.Fatalf("got %v, want %v (+/- %v)", got, tc.want, tc.tolerance)
			}
		})
	}
}

func TestFallbackRoute_AppliesCorrectionFactorAndAverageSpeed(t *testing.T) {
	config := Config{
		DistanceCorrectionFactor: 1.3,
		AverageSpeedKmh:          30,
	}

	route := fallbackRoute(config, 0, 0, 0, 1) // ~111.195 km straight-line

	wantDistance := 111.195 * 1.3
	if !almostEqual(route.DistanceKm, wantDistance, 0.5) {
		t.Fatalf("got distance %v, want ~%v", route.DistanceKm, wantDistance)
	}

	wantDuration := (wantDistance / 30) * 60
	if !almostEqual(route.DurationMinutes, wantDuration, 0.5) {
		t.Fatalf("got duration %v, want ~%v", route.DurationMinutes, wantDuration)
	}

	if !route.Estimated {
		t.Fatal("expected a fallback route to be flagged Estimated")
	}
}

// --- baseFareBreakdown -------------------------------------------------

func TestBaseFareBreakdown(t *testing.T) {
	config := Config{
		CurrencyCode:  "IQD",
		BaseFare:      decimal.NewFromInt(1000),
		PerKmRate:     decimal.NewFromInt(250),
		PerMinuteRate: decimal.NewFromInt(100),
	}
	route := Route{DistanceKm: 10, DurationMinutes: 20}

	got := baseFareBreakdown(config, route)

	if !got.DistanceFare.Equal(decimal.NewFromInt(2500)) {
		t.Fatalf("got distance fare %v, want 2500", got.DistanceFare)
	}
	if !got.DurationFare.Equal(decimal.NewFromInt(2000)) {
		t.Fatalf("got duration fare %v, want 2000", got.DurationFare)
	}
	if !got.Subtotal.Equal(decimal.NewFromInt(5500)) { // 1000 + 2500 + 2000
		t.Fatalf("got subtotal %v, want 5500", got.Subtotal)
	}
	if got.CurrencyCode != "IQD" {
		t.Fatalf("got currency %q", got.CurrencyCode)
	}
}

// --- timeInWindow -----------------------------------------------------

func TestTimeInWindow(t *testing.T) {
	cases := []struct {
		name    string
		current string
		start   string
		end     string
		want    bool
	}{
		{"ordinary window, inside", "08:00:00", "07:00:00", "09:00:00", true},
		{"ordinary window, before start", "06:59:59", "07:00:00", "09:00:00", false},
		{"ordinary window, after end", "09:00:01", "07:00:00", "09:00:00", false},
		{"ordinary window, exactly at boundaries", "07:00:00", "07:00:00", "09:00:00", true},
		{"overnight window, late night side", "23:30:00", "22:00:00", "02:00:00", true},
		{"overnight window, early morning side", "01:30:00", "22:00:00", "02:00:00", true},
		{"overnight window, outside", "12:00:00", "22:00:00", "02:00:00", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := timeInWindow(tc.current, tc.start, tc.end)
			if got != tc.want {
				t.Fatalf("timeInWindow(%s, %s, %s) = %v, want %v", tc.current, tc.start, tc.end, got, tc.want)
			}
		})
	}
}

// --- timeOfDaySurgePercent ----------------------------------------------

func TestTimeOfDaySurgePercent_TakesHighestMatchingRule(t *testing.T) {
	// 2026-09-15 is a Tuesday (weekday 2).
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	tuesday := 2

	rules := []SurgeTimeRule{
		{StartTime: "07:00:00", EndTime: "09:00:00", SurgePercent: decimal.NewFromInt(30), Active: true},                     // every day, matches
		{DayOfWeek: &tuesday, StartTime: "07:00:00", EndTime: "09:00:00", SurgePercent: decimal.NewFromInt(60), Active: true}, // matches, higher
		{DayOfWeek: intPtr(3), StartTime: "07:00:00", EndTime: "09:00:00", SurgePercent: decimal.NewFromInt(90), Active: true}, // wrong day
		{StartTime: "12:00:00", EndTime: "13:00:00", SurgePercent: decimal.NewFromInt(200), Active: true},                     // wrong time
	}

	got := timeOfDaySurgePercent(rules, now)
	if !got.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("got %v, want 60 (the highest matching rule)", got)
	}
}

func TestTimeOfDaySurgePercent_NoMatchingRuleIsZero(t *testing.T) {
	now := time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC)

	rules := []SurgeTimeRule{
		{StartTime: "07:00:00", EndTime: "09:00:00", SurgePercent: decimal.NewFromInt(30), Active: true},
	}

	if got := timeOfDaySurgePercent(rules, now); !got.IsZero() {
		t.Fatalf("got %v, want 0", got)
	}
}

func intPtr(v int) *int { return &v }

// --- Coupon.IsCurrentlyValid ----------------------------------------------

func TestCoupon_IsCurrentlyValid(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	base := Coupon{
		Active:     true,
		ValidFrom:  now.Add(-24 * time.Hour),
		ValidUntil: now.Add(24 * time.Hour),
	}

	t.Run("valid within window", func(t *testing.T) {
		if !base.IsCurrentlyValid(now) {
			t.Fatal("expected coupon to be valid")
		}
	})

	t.Run("inactive", func(t *testing.T) {
		c := base
		c.Active = false
		if c.IsCurrentlyValid(now) {
			t.Fatal("expected an inactive coupon to be invalid")
		}
	})

	t.Run("not yet started", func(t *testing.T) {
		c := base
		c.ValidFrom = now.Add(time.Hour)
		if c.IsCurrentlyValid(now) {
			t.Fatal("expected a not-yet-started coupon to be invalid")
		}
	})

	t.Run("expired", func(t *testing.T) {
		c := base
		c.ValidUntil = now.Add(-time.Hour)
		if c.IsCurrentlyValid(now) {
			t.Fatal("expected an expired coupon to be invalid")
		}
	})

	t.Run("redemption cap reached", func(t *testing.T) {
		c := base
		max := 10
		c.MaxRedemptions = &max
		c.RedemptionCount = 10
		if c.IsCurrentlyValid(now) {
			t.Fatal("expected a fully-redeemed coupon to be invalid")
		}
	})

	t.Run("redemption cap not reached", func(t *testing.T) {
		c := base
		max := 10
		c.MaxRedemptions = &max
		c.RedemptionCount = 9
		if !c.IsCurrentlyValid(now) {
			t.Fatal("expected a coupon under its redemption cap to be valid")
		}
	})
}
