package config

import (
	"testing"
	"time"
)

func TestParseDurationsReturnsParsedValues(t *testing.T) {
	cfg := Config{
		OTPChallengeTTL: "5m",
		AccessTokenTTL:  "15m",
		SessionTTL:      "720h",
		RefreshTokenTTL: "696h",
	}

	durations, err := ParseDurations(cfg)
	if err != nil {
		t.Fatalf("ParseDurations() returned an error: %v", err)
	}

	if durations.OTPChallengeTTL != 5*time.Minute {
		t.Fatalf(
			"OTPChallengeTTL is %v, expected %v",
			durations.OTPChallengeTTL,
			5*time.Minute,
		)
	}

	if durations.AccessTokenTTL != 15*time.Minute {
		t.Fatalf(
			"AccessTokenTTL is %v, expected %v",
			durations.AccessTokenTTL,
			15*time.Minute,
		)
	}

	if durations.SessionTTL != 720*time.Hour {
		t.Fatalf(
			"SessionTTL is %v, expected %v",
			durations.SessionTTL,
			720*time.Hour,
		)
	}

	if durations.RefreshTokenTTL != 696*time.Hour {
		t.Fatalf(
			"RefreshTokenTTL is %v, expected %v",
			durations.RefreshTokenTTL,
			696*time.Hour,
		)
	}
}

func TestParseDurationsRejectsInvalidDuration(t *testing.T) {
	cfg := Config{
		OTPChallengeTTL: "invalid",
		AccessTokenTTL:  "15m",
		SessionTTL:      "720h",
		RefreshTokenTTL: "696h",
	}

	_, err := ParseDurations(cfg)
	if err == nil {
		t.Fatal(
			"ParseDurations() returned nil error for invalid duration",
		)
	}
}

func TestParseDurationsRejectsNonPositiveDuration(t *testing.T) {
	cfg := Config{
		OTPChallengeTTL: "0s",
		AccessTokenTTL:  "15m",
		SessionTTL:      "720h",
		RefreshTokenTTL: "696h",
	}

	_, err := ParseDurations(cfg)
	if err == nil {
		t.Fatal(
			"ParseDurations() returned nil error for zero duration",
		)
	}
}

func TestParseDurationsRejectsRefreshTokenTTLGreaterThanSessionTTL(
	t *testing.T,
) {
	cfg := Config{
		OTPChallengeTTL: "5m",
		AccessTokenTTL:  "15m",
		SessionTTL:      "24h",
		RefreshTokenTTL: "48h",
	}

	_, err := ParseDurations(cfg)
	if err == nil {
		t.Fatal(
			"ParseDurations() allowed refresh token TTL to exceed session TTL",
		)
	}
}