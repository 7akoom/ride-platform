package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// AccountDeletion is how accounts are deleted: the grace period, the
// eraser's pace, and the services that say what stands in the way (trips,
// money) and hold the person's files.
type AccountDeletion struct {
	GracePeriod   time.Duration
	CheckInterval time.Duration
	RetryAfter    time.Duration

	RiderServiceAddress  string
	DriverServiceAddress string
	TripServiceAddress   string
	WalletServiceAddress string
	MediaServiceAddress  string
}

// LoadAccountDeletion reads ACCOUNT_DELETION_* and the *_SERVICE_ADDRESS
// settings. The grace period is at least a day (30 days by default).
func LoadAccountDeletion() (AccountDeletion, error) {
	out := AccountDeletion{
		RiderServiceAddress:  strings.TrimSpace(getEnv("RIDER_SERVICE_ADDRESS", "localhost:50052")),
		DriverServiceAddress: strings.TrimSpace(getEnv("DRIVER_SERVICE_ADDRESS", "localhost:50053")),
		TripServiceAddress:   strings.TrimSpace(getEnv("TRIP_SERVICE_ADDRESS", "localhost:50055")),
		WalletServiceAddress: strings.TrimSpace(getEnv("WALLET_SERVICE_ADDRESS", "localhost:50058")),
		MediaServiceAddress:  strings.TrimSpace(getEnv("MEDIA_SERVICE_ADDRESS", "localhost:50062")),
	}

	durations := []struct {
		name     string
		fallback string
		min      time.Duration
		target   *time.Duration
	}{
		{"ACCOUNT_DELETION_GRACE_PERIOD", "720h", 24 * time.Hour, &out.GracePeriod},
		{"ACCOUNT_DELETION_CHECK_INTERVAL", "1m", time.Second, &out.CheckInterval},
		{"ACCOUNT_DELETION_RETRY_AFTER", "1h", time.Second, &out.RetryAfter},
	}

	for _, d := range durations {
		value := strings.TrimSpace(os.Getenv(d.name))
		if value == "" {
			value = d.fallback
		}

		parsed, err := time.ParseDuration(value)
		if err != nil {
			return AccountDeletion{}, fmt.Errorf("%s has invalid duration %q: %w", d.name, value, err)
		}

		if parsed < d.min {
			return AccountDeletion{}, fmt.Errorf("%s must be at least %s", d.name, d.min)
		}

		*d.target = parsed
	}

	for name, address := range map[string]string{
		"RIDER_SERVICE_ADDRESS":  out.RiderServiceAddress,
		"DRIVER_SERVICE_ADDRESS": out.DriverServiceAddress,
		"TRIP_SERVICE_ADDRESS":   out.TripServiceAddress,
		"WALLET_SERVICE_ADDRESS": out.WalletServiceAddress,
		"MEDIA_SERVICE_ADDRESS":  out.MediaServiceAddress,
	} {
		if address == "" {
			return AccountDeletion{}, fmt.Errorf("%s is required", name)
		}
	}

	return out, nil
}
