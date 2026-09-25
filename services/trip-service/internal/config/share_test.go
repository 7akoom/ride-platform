package config

import (
	"testing"
	"time"
)

func TestParseShare(t *testing.T) {
	got, err := ParseShare(Config{ShareMaxAge: "12h", ShareAfterEnd: "30m"})
	if err != nil || got.URLBase != "" || got.MaxAge != 12*time.Hour || got.AfterEnd != 30*time.Minute {
		t.Fatalf("defaults %+v %v", got, err)
	}

	got, err = ParseShare(Config{ShareURLBase: " https://ride.example/t/ ", ShareMaxAge: "2h", ShareAfterEnd: "0s"})
	if err != nil || got.URLBase != "https://ride.example/t/" || got.AfterEnd != 0 {
		t.Fatalf("set %+v %v", got, err)
	}

	for name, cfg := range map[string]Config{
		"not a URL":       {ShareURLBase: "ride.example/t/", ShareMaxAge: "12h", ShareAfterEnd: "30m"},
		"another scheme":  {ShareURLBase: "ftp://ride.example/", ShareMaxAge: "12h", ShareAfterEnd: "30m"},
		"under an hour":   {ShareMaxAge: "30m", ShareAfterEnd: "30m"},
		"over two days":   {ShareMaxAge: "72h", ShareAfterEnd: "30m"},
		"a day after end": {ShareMaxAge: "12h", ShareAfterEnd: "24h"},
		"not a duration":  {ShareMaxAge: "twelve hours", ShareAfterEnd: "30m"},
	} {
		if _, err := ParseShare(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
