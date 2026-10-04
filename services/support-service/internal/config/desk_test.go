package config

import (
	"testing"
	"time"
)

func TestParseDesk(t *testing.T) {
	cfg := Load()

	desk, err := ParseDesk(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if desk.FirstResponse["urgent"] != 15*time.Minute || desk.FirstResponse["low"] != 24*time.Hour ||
		desk.AutoResolveAfter != 72*time.Hour || desk.AutoCloseAfter != 168*time.Hour {
		t.Fatalf("defaults: %+v", desk)
	}

	cfg.AutoCloseAfter = "0"
	if desk, err = ParseDesk(cfg); err != nil || desk.AutoCloseAfter != 0 {
		t.Fatalf("switched off: %v %v", desk.AutoCloseAfter, err)
	}

	for _, bad := range []string{"urgent=0s", "soon=1h", "urgent", "high=1y"} {
		cfg := Load()
		cfg.FirstResponseTargets = bad

		if _, err := ParseDesk(cfg); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}

	cfg = Load()
	cfg.AutoResolveAfter = "-1h"

	if _, err := ParseDesk(cfg); err == nil {
		t.Error("a negative auto-resolve accepted")
	}
}
