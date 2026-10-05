package query

import (
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

func TestResolveWindowUsesLocalDays(t *testing.T) {
	baghdad, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		t.Fatal(err)
	}

	// 22:30 UTC on 4 Oct is already 5 Oct in Baghdad.
	now := time.Date(2026, 10, 4, 22, 30, 0, 0, time.UTC)

	w, err := ResolveWindow(domain.DateRange{}, baghdad, now)
	if err != nil {
		t.Fatal(err)
	}

	if got := w.To.Format(time.DateOnly); got != "2026-10-05" {
		t.Fatalf("default to_date = %s, want today in Baghdad", got)
	}

	if w.Days() != 30 || w.From.Format(time.DateOnly) != "2026-09-06" {
		t.Fatalf("default range %s..%s (%d days)", w.From.Format(time.DateOnly), w.To.Format(time.DateOnly), w.Days())
	}

	w, err = ResolveWindow(domain.DateRange{From: "2026-10-01", To: "2026-10-02"}, baghdad, now)
	if err != nil {
		t.Fatal(err)
	}

	if !w.Start.Equal(time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)) || !w.End.Equal(time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC)) {
		t.Fatalf("instants %s .. %s", w.Start.UTC(), w.End.UTC())
	}

	// Timestamps count as the local day they fall on.
	w, err = ResolveWindow(domain.DateRange{Start: time.Date(2026, 9, 30, 21, 30, 0, 0, time.UTC), End: now}, baghdad, now)
	if err != nil {
		t.Fatal(err)
	}

	if w.From.Format(time.DateOnly) != "2026-10-01" || w.To.Format(time.DateOnly) != "2026-10-05" {
		t.Fatalf("timestamps gave %s..%s", w.From.Format(time.DateOnly), w.To.Format(time.DateOnly))
	}

	// Dates win over timestamps.
	w, err = ResolveWindow(domain.DateRange{From: "2026-10-03", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, baghdad, now)
	if err != nil || w.From.Format(time.DateOnly) != "2026-10-03" {
		t.Fatalf("from_date ignored: %v %v", w.From, err)
	}
}

func TestResolveWindowRefusesBadRanges(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	for name, rng := range map[string]domain.DateRange{
		"not a date":   {From: "05/10/2026"},
		"backwards":    {From: "2026-10-05", To: "2026-10-04"},
		"too long":     {From: "2025-01-01", To: "2026-10-05"},
		"bad to_date":  {To: "2026-13-01"},
		"from > today": {From: "2026-10-06"},
	} {
		if _, err := ResolveWindow(rng, time.UTC, now); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	if _, err := ResolveWindow(domain.DateRange{From: "2025-10-05", To: "2026-10-05"}, time.UTC, now); err != nil {
		t.Errorf("366 days must be accepted: %v", err)
	}
}

func TestMondayOf(t *testing.T) {
	for day, want := range map[string]string{
		"2026-10-05": "2026-10-05", // Monday
		"2026-10-11": "2026-10-05", // Sunday
		"2026-10-07": "2026-10-05",
	} {
		parsed, _ := time.Parse(time.DateOnly, day)
		if got := MondayOf(parsed).Format(time.DateOnly); got != want {
			t.Errorf("MondayOf(%s) = %s, want %s", day, got, want)
		}
	}
}

func TestScopeIsChecked(t *testing.T) {
	for name, scope := range map[string]domain.Scope{
		"city":  {CityID: "erbil"},
		"zone":  {ZoneID: "1; drop table"},
		"class": {VehicleClass: "Comfort Plus"},
	} {
		if err := validateScope(scope); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	if err := validateScope(domain.Scope{CityID: "0b8f3c1e-8a55-4c3e-9a51-3f0f6f1b2c3d", VehicleClass: "economy"}); err != nil {
		t.Errorf("valid scope refused: %v", err)
	}
}
