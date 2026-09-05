package config

import (
	"fmt"
	"strconv"
	"time"
)

type OTPRequestRateLimit struct {
	Cooldown          time.Duration
	Window            time.Duration
	MaxRequests       int
	SourceWindow      time.Duration
	SourceMaxRequests int
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

	maxRequests, err := parsePositiveInteger(
		"OTP_REQUEST_MAX_REQUESTS",
		cfg.OTPRequestMaxRequests,
	)
	if err != nil {
		return OTPRequestRateLimit{}, err
	}

	sourceWindow, err := parsePositiveDuration(
		"OTP_REQUEST_SOURCE_WINDOW",
		cfg.OTPRequestSourceWindow,
	)
	if err != nil {
		return OTPRequestRateLimit{}, err
	}

	sourceMaxRequests, err := parsePositiveInteger(
		"OTP_REQUEST_SOURCE_MAX_REQUESTS",
		cfg.OTPRequestSourceMaxRequests,
	)
	if err != nil {
		return OTPRequestRateLimit{}, err
	}

	if cooldown > window {
		return OTPRequestRateLimit{}, fmt.Errorf(
			"OTP_REQUEST_COOLDOWN cannot exceed OTP_REQUEST_WINDOW",
		)
	}

	return OTPRequestRateLimit{
		Cooldown:          cooldown,
		Window:            window,
		MaxRequests:       maxRequests,
		SourceWindow:      sourceWindow,
		SourceMaxRequests: sourceMaxRequests,
	}, nil
}

func parsePositiveInteger(
	name string,
	value string,
) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%s has invalid integer %q: %w",
			name,
			value,
			err,
		)
	}

	if parsed <= 0 {
		return 0, fmt.Errorf(
			"%s must be greater than zero",
			name,
		)
	}

	return parsed, nil
}
