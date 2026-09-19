package config

import "os"

type Config struct {
	ServiceName            string
	Environment            string
	GRPCAddress            string
	MetricsAddress         string
	TripServiceAddress     string
	LocationServiceAddress string
	DriverServiceAddress   string
	WalletServiceAddress   string

	// Verifies access tokens issued by identity-service. The public key
	// must be copied from identity-service's own .local/keys directory —
	// it is never committed to the repo.
	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	// Shared secret this service presents to trip/location/driver/wallet
	// when calling them — dispatch is a machine caller, not an end user,
	// so it authenticates as a service rather than forwarding a token it
	// doesn't have. Must be identical across every service in a
	// deployment; treat it like a password — the checked-in default is
	// for local dev only.
	InternalServiceToken string

	// Per-caller token-bucket rate limit (see
	// transport/grpc/rate_limit_interceptor.go). Defaults are generous
	// for a single mobile-app client under normal use, tight enough to
	// stop a runaway retry loop or a naive script.
	RateLimitRequestsPerSecond string
	RateLimitBurst             string

	// NATS connection used to consume trip.requested and dispatch a
	// driver automatically. This service only consumes; it never
	// publishes, so there is no outbox here.
	NATSURL            string
	NATSConnectTimeout string
	NATSReconnectWait  string
	NATSDrainTimeout   string

	// Auto-dispatch behaviour (see config/auto_dispatch.go).
	DispatchRetryInterval string
	DispatchSearchTimeout string
}

func Load() Config {
	return Config{
		ServiceName:            getEnv("SERVICE_NAME", "dispatch-service"),
		Environment:            getEnv("ENVIRONMENT", "development"),
		GRPCAddress:            getEnv("GRPC_ADDRESS", ":50056"),
		MetricsAddress:         getEnv("METRICS_ADDRESS", ":9096"),
		TripServiceAddress:     getEnv("TRIP_SERVICE_ADDRESS", "localhost:50055"),
		LocationServiceAddress: getEnv("LOCATION_SERVICE_ADDRESS", "localhost:50054"),
		DriverServiceAddress:   getEnv("DRIVER_SERVICE_ADDRESS", "localhost:50053"),
		WalletServiceAddress:   getEnv("WALLET_SERVICE_ADDRESS", "localhost:50058"),

		AccessTokenPublicKeyPath: getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ".local/keys/access_token_public.pem"),
		AccessTokenIssuer:        getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:      getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:         getEnv("ACCESS_TOKEN_KEY_ID", "identity-dev-1"),

		InternalServiceToken: getEnv("INTERNAL_SERVICE_TOKEN", "dev-internal-service-token-change-me"),

		RateLimitRequestsPerSecond: getEnv("RATE_LIMIT_REQUESTS_PER_SECOND", "20"),
		RateLimitBurst:             getEnv("RATE_LIMIT_BURST", "40"),

		NATSURL:            getEnv("NATS_URL", "nats://localhost:4222"),
		NATSConnectTimeout: getEnv("NATS_CONNECT_TIMEOUT", "5s"),
		NATSReconnectWait:  getEnv("NATS_RECONNECT_WAIT", "2s"),
		NATSDrainTimeout:   getEnv("NATS_DRAIN_TIMEOUT", "10s"),

		DispatchRetryInterval: getEnv("DISPATCH_RETRY_INTERVAL", "5s"),
		DispatchSearchTimeout: getEnv("DISPATCH_SEARCH_TIMEOUT", "2m"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
