package config

import (
	"testing"
	"time"
)

func TestParseOTPRequestRateLimitReturnsParsedValues(t *testing.T) {
	cfg := Config{
		OTPRequestCooldown:    "60s",
		OTPRequestWindow:      "15m",
		OTPRequestMaxRequests: "5",
	}

	rateLimit, err := ParseOTPRequestRateLimit(cfg)
	if err != nil {
		t.Fatalf(
			"ParseOTPRequestRateLimit() returned an error: %v",
			err,
		)
	}

	if rateLimit.Cooldown != time.Minute {
		t.Fatalf(
			"Cooldown is %v, expected %v",
			rateLimit.Cooldown,
			time.Minute,
		)
	}

	if rateLimit.Window != 15*time.Minute {
		t.Fatalf(
			"Window is %v, expected %v",
			rateLimit.Window,
			15*time.Minute,
		)
	}

	if rateLimit.MaxRequests != 5 {
		t.Fatalf(
			"MaxRequests is %d, expected 5",
			rateLimit.MaxRequests,
		)
	}
}

func TestParseOTPRequestRateLimitRejectsInvalidCooldown(
	t *testing.T,
) {
	cfg := Config{
		OTPRequestCooldown:    "invalid",
		OTPRequestWindow:      "15m",
		OTPRequestMaxRequests: "5",
	}

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() returned nil error for invalid cooldown",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsInvalidWindow(
	t *testing.T,
) {
	cfg := Config{
		OTPRequestCooldown:    "60s",
		OTPRequestWindow:      "invalid",
		OTPRequestMaxRequests: "5",
	}

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() returned nil error for invalid window",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsInvalidMaxRequests(
	t *testing.T,
) {
	cfg := Config{
		OTPRequestCooldown:    "60s",
		OTPRequestWindow:      "15m",
		OTPRequestMaxRequests: "abc",
	}

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() returned nil error for invalid max requests",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsNonPositiveMaxRequests(
	t *testing.T,
) {
	cfg := Config{
		OTPRequestCooldown:    "60s",
		OTPRequestWindow:      "15m",
		OTPRequestMaxRequests: "0",
	}

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() allowed zero max requests",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsCooldownGreaterThanWindow(
	t *testing.T,
) {
	cfg := Config{
		OTPRequestCooldown:    "30m",
		OTPRequestWindow:      "15m",
		OTPRequestMaxRequests: "5",
	}

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() allowed cooldown greater than window",
		)
	}
}