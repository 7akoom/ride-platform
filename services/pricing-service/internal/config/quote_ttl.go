package config

import (
	"fmt"
	"strings"
	"time"
)

// Bounds on QUOTE_TTL: long enough for a rider to look at the prices and
// pick, short enough that a quoted price still reflects the street.
const (
	minQuoteTTL = time.Minute
	maxQuoteTTL = 30 * time.Minute
)

// ParseQuoteTTL reads QUOTE_TTL, how long a fare quote holds its price.
func ParseQuoteTTL(cfg Config) (time.Duration, error) {
	ttl, err := time.ParseDuration(strings.TrimSpace(cfg.QuoteTTL))
	if err != nil {
		return 0, fmt.Errorf("QUOTE_TTL has invalid value %q (for example 5m): %w", cfg.QuoteTTL, err)
	}

	if ttl < minQuoteTTL || ttl > maxQuoteTTL {
		return 0, fmt.Errorf("QUOTE_TTL must be between %s and %s, got %s", minQuoteTTL, maxQuoteTTL, ttl)
	}

	return ttl, nil
}
