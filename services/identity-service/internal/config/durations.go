package config

import (
	"fmt"
	"time"
)

type Durations struct {
	OTPChallengeTTL time.Duration
	AccessTokenTTL  time.Duration
	SessionTTL      time.Duration
	RefreshTokenTTL time.Duration
}

func ParseDurations(cfg Config) (Durations, error) {
	otpChallengeTTL, err := parsePositiveDuration(
		"OTP_CHALLENGE_TTL",
		cfg.OTPChallengeTTL,
	)
	if err != nil {
		return Durations{}, err
	}

	accessTokenTTL, err := parsePositiveDuration(
		"ACCESS_TOKEN_TTL",
		cfg.AccessTokenTTL,
	)
	if err != nil {
		return Durations{}, err
	}

	sessionTTL, err := parsePositiveDuration(
		"SESSION_TTL",
		cfg.SessionTTL,
	)
	if err != nil {
		return Durations{}, err
	}

	refreshTokenTTL, err := parsePositiveDuration(
		"REFRESH_TOKEN_TTL",
		cfg.RefreshTokenTTL,
	)
	if err != nil {
		return Durations{}, err
	}

	if refreshTokenTTL > sessionTTL {
		return Durations{}, fmt.Errorf(
			"REFRESH_TOKEN_TTL cannot exceed SESSION_TTL",
		)
	}

	return Durations{
		OTPChallengeTTL: otpChallengeTTL,
		AccessTokenTTL:  accessTokenTTL,
		SessionTTL:      sessionTTL,
		RefreshTokenTTL: refreshTokenTTL,
	}, nil
}

func parsePositiveDuration(
	name string,
	value string,
) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%s has invalid duration %q: %w",
			name,
			value,
			err,
		)
	}

	if duration <= 0 {
		return 0, fmt.Errorf(
			"%s must be greater than zero",
			name,
		)
	}

	return duration, nil
}