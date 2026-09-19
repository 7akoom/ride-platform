package pricing

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestRoundToIncrement(t *testing.T) {
	cases := []struct {
		name      string
		amount    string
		increment string
		want      string
	}{
		{"rounds up to nearest 250", "17191.965", "250", "17250"},
		{"rounds down to nearest 250", "17124.99", "250", "17000"},
		{"exact half rounds up", "17125", "250", "17250"},
		{"already a multiple stays put", "17000", "250", "17000"},
		{"just below half a step rounds down", "8562.4999", "250", "8500"},
		{"whole dinar increment", "17191.965", "1", "17192"},
		{"thousand increment", "17191.965", "1000", "17000"},
		{"tiny positive fare never becomes free", "100", "250", "250"},
		{"just under half a step still not free", "124.99", "250", "250"},
		{"exactly half a step", "125", "250", "250"},
		{"zero total stays zero", "0", "250", "0"},
		{"zero increment disables rounding", "17191.965", "0", "17191.965"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundToIncrement(
				decimal.RequireFromString(tc.amount),
				decimal.RequireFromString(tc.increment),
			)

			if !got.Equal(decimal.RequireFromString(tc.want)) {
				t.Fatalf("roundToIncrement(%s, %s) = %s, want %s", tc.amount, tc.increment, got, tc.want)
			}
		})
	}
}

func TestRoundToIncrementNegativeIncrementLeavesAmountUntouched(t *testing.T) {
	amount := decimal.RequireFromString("17191.965")

	got := roundToIncrement(amount, decimal.RequireFromString("-250"))
	if !got.Equal(amount) {
		t.Fatalf("expected the amount untouched, got %s", got)
	}
}

func TestRoundedFareIsAlwaysAWholeMultiple(t *testing.T) {
	increment := decimal.RequireFromString("250")

	for cents := int64(1); cents <= 2_000_000; cents += 7919 {
		amount := decimal.New(cents, -2)
		rounded := roundToIncrement(amount, increment)

		if !rounded.Mod(increment).IsZero() {
			t.Fatalf("%s rounded to %s, which is not a multiple of 250", amount, rounded)
		}

		if rounded.IsZero() {
			t.Fatalf("%s rounded to zero", amount)
		}
	}
}

func TestWithFareRoundingSetsTheServiceIncrement(t *testing.T) {
	s := &service{}

	WithFareRounding(decimal.RequireFromString("250"))(s)

	if !s.fareRoundingIncrement.Equal(decimal.RequireFromString("250")) {
		t.Fatalf("expected increment 250, got %s", s.fareRoundingIncrement)
	}
}
