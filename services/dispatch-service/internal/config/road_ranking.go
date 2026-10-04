package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// maxMaxPickup bounds DISPATCH_MAX_PICKUP_ETA: past two hours it is a typo.
const maxMaxPickup = 2 * time.Hour

// RoadRanking is how dispatch orders nearby drivers (see dispatch.WithRoadRanking).
type RoadRanking struct {
	// Enabled ranks drivers by time to the pickup by road (OSRM) instead of by
	// straight-line distance.
	Enabled bool

	// MaxPickup leaves out drivers further than it by road; 0 keeps them all.
	MaxPickup time.Duration
}

// ParseRoadRanking reads DISPATCH_ROAD_RANKING (true by default) and
// DISPATCH_MAX_PICKUP_ETA (20m by default, 0 for no limit, at most 2h).
func ParseRoadRanking(cfg Config) (RoadRanking, error) {
	enabled := true

	if value := strings.TrimSpace(cfg.DispatchRoadRanking); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return RoadRanking{}, fmt.Errorf("DISPATCH_ROAD_RANKING must be true or false, got %q", cfg.DispatchRoadRanking)
		}

		enabled = parsed
	}

	maxPickup := 20 * time.Minute

	if value := strings.TrimSpace(cfg.DispatchMaxPickupETA); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return RoadRanking{}, fmt.Errorf("DISPATCH_MAX_PICKUP_ETA has invalid duration %q: %w", cfg.DispatchMaxPickupETA, err)
		}

		if parsed < 0 || parsed > maxMaxPickup {
			return RoadRanking{}, fmt.Errorf("DISPATCH_MAX_PICKUP_ETA must be between 0 (no limit) and %s, got %s", maxMaxPickup, parsed)
		}

		maxPickup = parsed
	}

	return RoadRanking{Enabled: enabled, MaxPickup: maxPickup}, nil
}
