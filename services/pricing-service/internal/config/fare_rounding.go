package config

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// ParseFareRoundingIncrement reads FARE_ROUNDING_INCREMENT. Zero is valid
// and disables rounding; a blank or negative value is a configuration
// mistake and fails startup instead of silently pricing wrongly.
func ParseFareRoundingIncrement(cfg Config) (decimal.Decimal, error) {
	value := strings.TrimSpace(cfg.FareRoundingIncrement)

	increment, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf(
			"FARE_ROUNDING_INCREMENT has invalid value %q (use 0 to disable rounding): %w",
			cfg.FareRoundingIncrement,
			err,
		)
	}

	if increment.IsNegative() {
		return decimal.Zero, fmt.Errorf("FARE_ROUNDING_INCREMENT must not be negative, got %q", cfg.FareRoundingIncrement)
	}

	return increment, nil
}
