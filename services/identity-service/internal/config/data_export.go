package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// DataExport is "Download your data": how often a person may ask, how long
// the file is kept, how often the maker looks, and the two services the
// account deletion settings do not already name.
type DataExport struct {
	MinInterval   time.Duration
	KeepFor       time.Duration
	CheckInterval time.Duration

	SupportServiceAddress      string
	NotificationServiceAddress string
}

func LoadDataExport() (DataExport, error) {
	out := DataExport{
		SupportServiceAddress:      strings.TrimSpace(getEnv("SUPPORT_SERVICE_ADDRESS", "localhost:50063")),
		NotificationServiceAddress: strings.TrimSpace(getEnv("NOTIFICATION_SERVICE_ADDRESS", "localhost:50059")),
	}

	for _, d := range []struct {
		name     string
		fallback string
		min      time.Duration
		target   *time.Duration
	}{
		{"DATA_EXPORT_MIN_INTERVAL", "24h", time.Minute, &out.MinInterval},
		{"DATA_EXPORT_KEEP_FOR", "168h", time.Hour, &out.KeepFor},
		{"DATA_EXPORT_CHECK_INTERVAL", "30s", time.Second, &out.CheckInterval},
	} {
		value := strings.TrimSpace(os.Getenv(d.name))
		if value == "" {
			value = d.fallback
		}

		parsed, err := time.ParseDuration(value)
		if err != nil {
			return DataExport{}, fmt.Errorf("%s has invalid duration %q: %w", d.name, value, err)
		}

		if parsed < d.min {
			return DataExport{}, fmt.Errorf("%s must be at least %s", d.name, d.min)
		}

		*d.target = parsed
	}

	if out.SupportServiceAddress == "" || out.NotificationServiceAddress == "" {
		return DataExport{}, fmt.Errorf("SUPPORT_SERVICE_ADDRESS and NOTIFICATION_SERVICE_ADDRESS are required")
	}

	return out, nil
}
