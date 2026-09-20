package config

import (
	"fmt"
	"strings"
	"time"
)

// The bounds trip-service accepts for an offer; a value outside would be silently
// clamped there, so it is refused here instead.
const (
	minOfferTTL = 5 * time.Second
	maxOfferTTL = 60 * time.Second
)

// ParseOfferTTL reads DISPATCH_OFFER_TTL: how long a driver has to accept a trip
// that dispatch offers them. Zero (the default) switches offers off: dispatch then
// assigns the trip to the nearest eligible driver at once, as it always has. Any
// other value must be between 5s and 60s.
func ParseOfferTTL(cfg Config) (time.Duration, error) {
	value := strings.TrimSpace(cfg.DispatchOfferTTL)
	if value == "" {
		return 0, nil
	}

	ttl, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("DISPATCH_OFFER_TTL has invalid duration %q: %w", cfg.DispatchOfferTTL, err)
	}

	switch {
	case ttl == 0:
		return 0, nil

	case ttl < minOfferTTL || ttl > maxOfferTTL:
		return 0, fmt.Errorf("DISPATCH_OFFER_TTL must be 0 (offers off) or between %s and %s, got %s", minOfferTTL, maxOfferTTL, ttl)
	}

	return ttl, nil
}
