package config

import "os"

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	DatabaseURL    string

	// identity-service, asked (with the caller's own token) which email
	// addresses it verified, when a staff member accepts an invitation.
	IdentityServiceAddress string

	// When there is no staff member at all, this address is invited as the
	// first owner at startup. Ignored once any staff member exists.
	BootstrapOwnerEmail string

	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	// Shared secret for service-to-service calls. Every other service uses it
	// to ask AuthorizeStaffAction before an admin RPC runs.
	InternalServiceToken string

	RateLimitRequestsPerSecond string
	RateLimitBurst             string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "staff-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50061"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9101"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),

		IdentityServiceAddress: getEnv("IDENTITY_SERVICE_ADDRESS", "localhost:50051"),
		BootstrapOwnerEmail:    getEnv("STAFF_BOOTSTRAP_OWNER_EMAIL", ""),

		AccessTokenPublicKeyPath: getEnv(
			"ACCESS_TOKEN_PUBLIC_KEY_PATH",
			".local/keys/access_token_public.pem",
		),

		AccessTokenIssuer: getEnv(
			"ACCESS_TOKEN_ISSUER",
			"ride-identity",
		),

		AccessTokenAudience: getEnv(
			"ACCESS_TOKEN_AUDIENCE",
			"ride-platform",
		),

		AccessTokenKeyID: getEnv(
			"ACCESS_TOKEN_KEY_ID",
			"identity-dev-1",
		),

		InternalServiceToken: getEnv(
			"INTERNAL_SERVICE_TOKEN",
			"dev-internal-service-token-change-me",
		),

		RateLimitRequestsPerSecond: getEnv(
			"RATE_LIMIT_REQUESTS_PER_SECOND",
			"20",
		),

		RateLimitBurst: getEnv(
			"RATE_LIMIT_BURST",
			"40",
		),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
