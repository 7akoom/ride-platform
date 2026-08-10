package config

import (
	"fmt"
	"strconv"
	"time"
)

type OTPRequestRateLimit struct {
	Cooldown    time.Duration
	Window      time.Duration
	MaxRequests int
}

func ParseOTPRequestRateLimit(
	cfg Config,
) (OTPRequestRateLimit, error) {
	cooldown, err := parsePositiveDuration(
		"OTP_REQUEST_COOLDOWN",
		cfg.OTPRequestCooldown,
	)
	if err != nil {
		return OTPRequestRateLimit{}, err
	}

	window, err := parsePositiveDuration(
		"OTP_REQUEST_WINDOW",
		cfg.OTPRequestWindow,
	)
	if err != nil {
		return OTPRequestRateLimit{}, err
	}

	maxRequests, err := strconv.Atoi(
		cfg.OTPRequestMaxRequests,
	)
	if err != nil {
		return OTPRequestRateLimit{}, fmt.Errorf(
			"OTP_REQUEST_MAX_REQUESTS has invalid integer %q: %w",
			cfg.OTPRequestMaxRequests,
			err,
		)
	}

	if maxRequests <= 0 {
		return OTPRequestRateLimit{}, fmt.Errorf(
			"OTP_REQUEST_MAX_REQUESTS must be greater than zero",
		)
	}

	if cooldown > window {
		return OTPRequestRateLimit{}, fmt.Errorf(
			"OTP_REQUEST_COOLDOWN cannot exceed OTP_REQUEST_WINDOW",
		)
	}

	return OTPRequestRateLimit{
		Cooldown:    cooldown,
		Window:      window,
		MaxRequests: maxRequests,
	}, nil
}