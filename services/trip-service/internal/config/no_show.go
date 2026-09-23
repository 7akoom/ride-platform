package config

import (
	"fmt"
	"strings"
	"time"
)

// A no-show wait shorter than a minute would let a driver cancel before a
// rider could reach the car; one over half an hour keeps them stuck there.
const (
	minNoShowWait = time.Minute
	maxNoShowWait = 30 * time.Minute
)

// ParseNoShowWait reads TRIP_NO_SHOW_WAIT.
func ParseNoShowWait(cfg Config) (time.Duration, error) {
	wait, err := time.ParseDuration(strings.TrimSpace(cfg.NoShowWait))
	if err != nil {
		return 0, fmt.Errorf("TRIP_NO_SHOW_WAIT has invalid value %q (for example 5m): %w", cfg.NoShowWait, err)
	}

	if wait < minNoShowWait || wait > maxNoShowWait {
		return 0, fmt.Errorf("TRIP_NO_SHOW_WAIT must be between %s and %s, got %s", minNoShowWait, maxNoShowWait, wait)
	}

	return wait, nil
}
