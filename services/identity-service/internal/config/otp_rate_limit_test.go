package config

import (
	"testing"
	"time"
)

func TestParseOTPRequestRateLimitReturnsParsedValues(t *testing.T) {
	cfg := validOTPRequestRateLimitConfig()

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

	if rateLimit.SourceWindow != 10*time.Minute {
		t.Fatalf(
			"SourceWindow is %v, expected %v",
			rateLimit.SourceWindow,
			10*time.Minute,
		)
	}

	if rateLimit.SourceMaxRequests != 30 {
		t.Fatalf(
			"SourceMaxRequests is %d, expected 30",
			rateLimit.SourceMaxRequests,
		)
	}
}

func TestParseOTPRequestRateLimitRejectsInvalidCooldown(
	t *testing.T,
) {
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestCooldown = "invalid"

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
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestWindow = "invalid"

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
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestMaxRequests = "abc"

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
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestMaxRequests = "0"

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
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestCooldown = "30m"

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() allowed cooldown greater than window",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsInvalidSourceWindow(
	t *testing.T,
) {
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestSourceWindow = "invalid"

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() returned nil error for invalid source window",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsNonPositiveSourceWindow(
	t *testing.T,
) {
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestSourceWindow = "0s"

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() allowed zero source window",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsInvalidSourceMaxRequests(
	t *testing.T,
) {
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestSourceMaxRequests = "abc"

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() returned nil error for invalid source max requests",
		)
	}
}

func TestParseOTPRequestRateLimitRejectsNonPositiveSourceMaxRequests(
	t *testing.T,
) {
	cfg := validOTPRequestRateLimitConfig()
	cfg.OTPRequestSourceMaxRequests = "0"

	_, err := ParseOTPRequestRateLimit(cfg)
	if err == nil {
		t.Fatal(
			"ParseOTPRequestRateLimit() allowed zero source max requests",
		)
	}
}

func validOTPRequestRateLimitConfig() Config {
	return Config{
		OTPRequestCooldown:          "60s",
		OTPRequestWindow:            "15m",
		OTPRequestMaxRequests:       "5",
		OTPRequestSourceWindow:      "10m",
		OTPRequestSourceMaxRequests: "30",
	}
}
