package config

import "os"

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	ValkeyAddress  string
	ValkeyPassword string

	// Persistent store for service zones (see internal/application/zone).
	// Everything else this service does is ephemeral, Valkey-only — this
	// is its first durable data.
	DatabaseURL string

	// Verifies access tokens issued by identity-service. The public key
	// must be copied from identity-service's own .local/keys directory —
	// it is never committed to the repo.
	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	// Shared secret for service-to-service calls (e.g. another service
	// acting on its own behalf, where there is no end-user access token
	// to check). Must be identical across every service in a deployment;
	// treat it like a password — the checked-in default is for local
	// dev only.
	InternalServiceToken string
	RiderServiceAddress  string
	DriverServiceAddress string

	// Per-caller token-bucket rate limit (see
	// transport/grpc/rate_limit_interceptor.go). Defaults are generous
	// for a single mobile-app client under normal use, tight enough to
	// stop a runaway retry loop or a naive script.
	RateLimitRequestsPerSecond string
	RateLimitBurst             string

	// Maps: the routing engine (OSRM) and the place search (Nominatim), both
	// self-hosted and built from OpenStreetMap (see config/maps.go).
	OSRMBaseURL      string
	NominatimBaseURL string
	MapsCountryCodes string
	MapsTimeout      string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "location-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50054"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9094"),
		ValkeyAddress:  getEnv("VALKEY_ADDRESS", ""),
		ValkeyPassword: getEnv("VALKEY_PASSWORD", ""),

		DatabaseURL: getEnv("DATABASE_URL", ""),

		AccessTokenPublicKeyPath: getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ".local/keys/access_token_public.pem"),
		AccessTokenIssuer:        getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:      getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:         getEnv("ACCESS_TOKEN_KEY_ID", "identity-dev-1"),

		InternalServiceToken: getEnv("INTERNAL_SERVICE_TOKEN", "dev-internal-service-token-change-me"),

		RiderServiceAddress:  getEnv("RIDER_SERVICE_ADDRESS", "localhost:50052"),
		DriverServiceAddress: getEnv("DRIVER_SERVICE_ADDRESS", "localhost:50053"),

		RateLimitRequestsPerSecond: getEnv("RATE_LIMIT_REQUESTS_PER_SECOND", "20"),
		RateLimitBurst:             getEnv("RATE_LIMIT_BURST", "40"),

		OSRMBaseURL:      getEnv("OSRM_BASE_URL", "http://localhost:5000"),
		NominatimBaseURL: getEnv("NOMINATIM_BASE_URL", "http://localhost:8088"),
		MapsCountryCodes: getEnv("MAPS_COUNTRY_CODES", "iq"),
		MapsTimeout:      getEnv("MAPS_HTTP_TIMEOUT", "5s"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
