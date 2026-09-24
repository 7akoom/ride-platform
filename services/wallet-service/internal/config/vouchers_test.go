package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseVouchers(t *testing.T) {
	strong := strings.Repeat("k", minVoucherCodeKeyLength)

	cases := []struct {
		name        string
		environment string
		key         string
		failures    string
		window      string
		wantErr     bool
	}{
		{"development runs with the placeholder", "development", developmentVoucherCodeKey, "5", "1h", false},
		{"production refuses the placeholder", "production", developmentVoucherCodeKey, "5", "1h", true},
		{"an unset environment is not development", "", developmentVoucherCodeKey, "5", "1h", true},
		{"production refuses a short key", "production", strings.Repeat("k", minVoucherCodeKeyLength-1), "5", "1h", true},
		{"production accepts a key of the minimum length", "production", strong, "5", "1h", false},
		{"an empty key is refused", "development", "", "5", "1h", true},
		{"no failures allowed is refused", "development", strong, "0", "1h", true},
		{"a failures count that is not a number", "development", strong, "many", "1h", true},
		{"a window under a minute", "development", strong, "5", "30s", true},
		{"a window that is not a duration", "development", strong, "5", "an hour", true},
	}

	for _, tc := range cases {
		got, err := ParseVouchers(Config{
			Environment:              tc.environment,
			VoucherCodeKey:           tc.key,
			VoucherRedeemMaxFailures: tc.failures,
			VoucherRedeemWindow:      tc.window,
		})

		if tc.wantErr != (err != nil) {
			t.Errorf("%s: err = %v", tc.name, err)

			continue
		}

		if err == nil && (got.MaxFailures != 5 || got.Window != time.Hour) {
			t.Errorf("%s: parsed %+v", tc.name, got)
		}

		if err != nil && tc.key != "" && strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s: the error leaks the key: %v", tc.name, err)
		}
	}
}
