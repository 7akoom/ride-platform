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

	// Verifies access tokens issued by identity-service. The public key
	// must be copied from identity-service's own .local/keys directory —
	// it is never committed to the repo.
	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	// Shared secret for service-to-service calls — this service's own
	// NATS event handler calls trip-service/driver-service with no
	// end-user access token in hand, so it authenticates as a service
	// instead. Must be identical across every service in a deployment;
	// treat it like a password — the checked-in default is for local
	// dev only.
	InternalServiceToken string

	// Per-caller token-bucket rate limit (see
	// transport/grpc/rate_limit_interceptor.go). Defaults are generous
	// for a single mobile-app client under normal use, tight enough to
	// stop a runaway retry loop or a naive script.
	RateLimitRequestsPerSecond string
	RateLimitBurst             string
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

		AccessTokenPublicKeyPath: getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ".local/keys/access_token_public.pem"),
		AccessTokenIssuer:        getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:      getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:         getEnv("ACCESS_TOKEN_KEY_ID", "identity-dev-1"),

		InternalServiceToken: getEnv("INTERNAL_SERVICE_TOKEN", "dev-internal-service-token-change-me"),

		RateLimitRequestsPerSecond: getEnv("RATE_LIMIT_REQUESTS_PER_SECOND", "20"),
		RateLimitBurst:             getEnv("RATE_LIMIT_BURST", "40"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
