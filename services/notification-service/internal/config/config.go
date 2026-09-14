package config

import (
	"os"
	"time"
)

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	DatabaseURL    string

	// Path to a Firebase service-account JSON key. Leave empty to run
	// without push — the service still records in-app notifications and
	// reports push as unconfigured rather than failing.
	FCMCredentialsFile string
	PushTimeout        time.Duration
}

func Load() Config {
	return Config{
		ServiceName:        getEnv("SERVICE_NAME", "notification-service"),
		Environment:        getEnv("ENVIRONMENT", "development"),
		GRPCAddress:        getEnv("GRPC_ADDRESS", ":50059"),
		MetricsAddress:     getEnv("METRICS_ADDRESS", ":9099"),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		FCMCredentialsFile: getEnv("FCM_CREDENTIALS_FILE", ""),
		PushTimeout:        10 * time.Second,
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
