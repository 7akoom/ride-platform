package config

import (
	"testing"
	"time"
)

func TestParseQuoteTTL(t *testing.T) {
	for input, want := range map[string]time.Duration{"5m": 5 * time.Minute, " 90s ": 90 * time.Second, "30m": 30 * time.Minute} {
		got, err := ParseQuoteTTL(Config{QuoteTTL: input})
		if err != nil || got != want {
			t.Fatalf("%q: got %s, %v", input, got, err)
		}
	}

	for _, input := range []string{"", "5", "30s", "31m", "-5m", "soon"} {
		if _, err := ParseQuoteTTL(Config{QuoteTTL: input}); err == nil {
			t.Fatalf("%q: expected an error", input)
		}
	}
}
