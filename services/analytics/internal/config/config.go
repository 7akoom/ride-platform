package config

import "os"

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	DatabaseURL    string

	NATSURL            string
	NATSConnectTimeout string
	NATSReconnectWait  string
	NATSDrainTimeout   string

	// Verifies access tokens issued by identity-service, for the query
	// gRPC API this service exposes to the Admin web app via the gateway.
	// This service never dials another service as a client — it only
	// consumes NATS events and answers queries — so unlike most other
	// services there is no InternalServiceToken use as an outbound caller,
	// only as a value other services could present to call in (kept for
	// consistency with the shared auth interceptor, unused for now).
	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string
	InternalServiceToken     string

	RateLimitRequestsPerSecond string
	RateLimitBurst             string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "analytics-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50060"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9100"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),

		NATSURL:            getEnv("NATS_URL", "nats://localhost:4222"),
		NATSConnectTimeout: getEnv("NATS_CONNECT_TIMEOUT", "5s"),
		NATSReconnectWait:  getEnv("NATS_RECONNECT_WAIT", "2s"),
		NATSDrainTimeout:   getEnv("NATS_DRAIN_TIMEOUT", "5s"),

		AccessTokenPublicKeyPath: getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ".local/keys/access_token_public.pem"),
		AccessTokenIssuer:        getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:      getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:         getEnv("ACCESS_TOKEN_KEY_ID", "identity-dev-1"),
		InternalServiceToken:     getEnv("INTERNAL_SERVICE_TOKEN", "dev-internal-service-token-change-me"),

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
