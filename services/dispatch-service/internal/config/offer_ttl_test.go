package config

import (
	"testing"
	"time"
)

func TestParseOfferTTL(t *testing.T) {
	cases := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"  ", 0, false},
		{"0", 0, false},
		{"0s", 0, false},
		{"15s", 15 * time.Second, false},
		{" 20s ", 20 * time.Second, false},
		{"5s", 5 * time.Second, false},
		{"60s", 60 * time.Second, false},
		{"1m", 60 * time.Second, false},
		{"4s", 0, true},
		{"61s", 0, true},
		{"2m", 0, true},
		{"-15s", 0, true},
		{"fifteen", 0, true},
		{"15", 0, true},
	}

	for _, tc := range cases {
		got, err := ParseOfferTTL(Config{DispatchOfferTTL: tc.value})

		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected an error, got %v", tc.value, got)
			}

			continue
		}

		if err != nil || got != tc.want {
			t.Errorf("%q: got %v, %v; want %v", tc.value, got, err, tc.want)
		}
	}
}
