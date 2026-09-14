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

	// Peers this service calls to enrich events before rendering a
	// notification (rider ID, driver name/vehicle, dropoff location).
	TripServiceAddress   string
	DriverServiceAddress string

	NATSURL            string
	NATSConnectTimeout string
	NATSReconnectWait  string
	NATSDrainTimeout   string
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

		TripServiceAddress:   getEnv("TRIP_SERVICE_ADDRESS", "localhost:50055"),
		DriverServiceAddress: getEnv("DRIVER_SERVICE_ADDRESS", "localhost:50053"),

		NATSURL:            getEnv("NATS_URL", "nats://localhost:4222"),
		NATSConnectTimeout: getEnv("NATS_CONNECT_TIMEOUT", "5s"),
		NATSReconnectWait:  getEnv("NATS_RECONNECT_WAIT", "2s"),
		NATSDrainTimeout:   getEnv("NATS_DRAIN_TIMEOUT", "5s"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
