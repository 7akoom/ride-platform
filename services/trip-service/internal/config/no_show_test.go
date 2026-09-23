package config

import (
	"testing"
	"time"
)

func TestParseNoShowWait(t *testing.T) {
	for input, want := range map[string]time.Duration{"5m": 5 * time.Minute, " 90s ": 90 * time.Second, "30m": 30 * time.Minute} {
		if got, err := ParseNoShowWait(Config{NoShowWait: input}); err != nil || got != want {
			t.Fatalf("%q: got %s, %v", input, got, err)
		}
	}

	for _, input := range []string{"", "5", "30s", "31m", "later"} {
		if _, err := ParseNoShowWait(Config{NoShowWait: input}); err == nil {
			t.Fatalf("%q: expected an error", input)
		}
	}
}
