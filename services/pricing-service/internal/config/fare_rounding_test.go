package config

import "testing"

func TestParseFareRoundingIncrement(t *testing.T) {
	valid := map[string]string{
		"250":   "250",
		" 250 ": "250",
		"1":     "1",
		"0":     "0",
		"0.5":   "0.5",
	}

	for input, want := range valid {
		got, err := ParseFareRoundingIncrement(Config{FareRoundingIncrement: input})
		if err != nil {
			t.Fatalf("%q: unexpected error %v", input, err)
		}

		if got.String() != want {
			t.Fatalf("%q: got %s, want %s", input, got, want)
		}
	}

	for _, input := range []string{"", "  ", "abc", "-250", "2,5"} {
		if _, err := ParseFareRoundingIncrement(Config{FareRoundingIncrement: input}); err == nil {
			t.Fatalf("%q: expected an error", input)
		}
	}
}

func TestLoadDefaultsFareRoundingToTwoHundredFifty(t *testing.T) {
	t.Setenv("FARE_ROUNDING_INCREMENT", "")

	cfg := Load()

	if cfg.FareRoundingIncrement != "250" {
		t.Fatalf("expected the default 250, got %q", cfg.FareRoundingIncrement)
	}
}
