package config

import (
	"fmt"
	"strings"
	"time"

	// Time zones are read from the binary, not the image: the service image
	// may have no zoneinfo.
	_ "time/tzdata"
)

// Incentives is how incentive campaigns are settled and how earnings days
// are cut.
type Incentives struct {
	// Location is the deployment's local time: earnings days and weeks, and
	// a campaign's daily hours when it names no time zone.
	Location *time.Location
	// SettleDelay is how long after a campaign ends it is paid, so trips
	// still finishing at the end are counted.
	SettleDelay time.Duration
	// CheckInterval is how often ended campaigns are looked for.
	CheckInterval time.Duration
}

func ParseIncentives(cfg Config) (Incentives, error) {
	name := strings.TrimSpace(cfg.WalletTimeZone)

	location, err := time.LoadLocation(name)
	if err != nil || name == "" {
		return Incentives{}, fmt.Errorf("WALLET_TIME_ZONE must be an IANA time zone, got %q", cfg.WalletTimeZone)
	}

	delay, err := time.ParseDuration(strings.TrimSpace(cfg.IncentiveSettleDelay))
	if err != nil || delay < 0 || delay > 24*time.Hour {
		return Incentives{}, fmt.Errorf("INCENTIVE_SETTLE_DELAY must be a duration from 0 to 24h, got %q", cfg.IncentiveSettleDelay)
	}

	interval, err := time.ParseDuration(strings.TrimSpace(cfg.IncentiveCheckInterval))
	if err != nil || interval < time.Second {
		return Incentives{}, fmt.Errorf("INCENTIVE_CHECK_INTERVAL must be a duration of at least 1s, got %q", cfg.IncentiveCheckInterval)
	}

	return Incentives{Location: location, SettleDelay: delay, CheckInterval: interval}, nil
}
