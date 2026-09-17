package config

import (
	"fmt"
	"strconv"
	"strings"
)

type RateLimit struct {
	RequestsPerSecond float64
	Burst             int
}

func ParseRateLimit(
	cfg Config,
) (RateLimit, error) {
	requestsPerSecond, err := strconv.ParseFloat(
		strings.TrimSpace(cfg.RateLimitRequestsPerSecond),
		64,
	)
	if err != nil {
		return RateLimit{}, fmt.Errorf(
			"RATE_LIMIT_REQUESTS_PER_SECOND has invalid number %q: %w",
			cfg.RateLimitRequestsPerSecond,
			err,
		)
	}

	if requestsPerSecond <= 0 {
		return RateLimit{}, fmt.Errorf(
			"RATE_LIMIT_REQUESTS_PER_SECOND must be greater than zero",
		)
	}

	burst, err := strconv.Atoi(
		strings.TrimSpace(cfg.RateLimitBurst),
	)
	if err != nil {
		return RateLimit{}, fmt.Errorf(
			"RATE_LIMIT_BURST has invalid integer %q: %w",
			cfg.RateLimitBurst,
			err,
		)
	}

	if burst <= 0 {
		return RateLimit{}, fmt.Errorf(
			"RATE_LIMIT_BURST must be greater than zero",
		)
	}

	return RateLimit{
		RequestsPerSecond: requestsPerSecond,
		Burst:             burst,
	}, nil
}
