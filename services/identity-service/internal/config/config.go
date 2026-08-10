package config

import "os"

type Config struct {
	ServiceName   string
	Environment   string
	GRPCAddress   string
	DatabaseURL   string
	OTPHashSecret string

	OTPChallengeTTL string

	OTPRequestCooldown    string
	OTPRequestWindow      string
	OTPRequestMaxRequests string

	AccessTokenPrivateKeyPath string
	AccessTokenPublicKeyPath  string
	AccessTokenIssuer         string
	AccessTokenAudience       string
	AccessTokenKeyID          string
	AccessTokenTTL            string

	SessionTTL      string
	RefreshTokenTTL string

	CleanupInterval          string
	OTPRequestEventRetention string
	OTPChallengeRetention    string
	AuthSessionRetention     string
}

func Load() Config {
	return Config{
		ServiceName:   "identity-service",
		Environment:   getEnv("APP_ENV", "development"),
		GRPCAddress:   getEnv("GRPC_ADDRESS", ":50051"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		OTPHashSecret: getEnv("OTP_HASH_SECRET", ""),

		OTPChallengeTTL: getEnv("OTP_CHALLENGE_TTL", "5m"),

		OTPRequestCooldown:    getEnv("OTP_REQUEST_COOLDOWN", "60s"),
		OTPRequestWindow:      getEnv("OTP_REQUEST_WINDOW", "15m"),
		OTPRequestMaxRequests: getEnv("OTP_REQUEST_MAX_REQUESTS", "5"),

		AccessTokenPrivateKeyPath: getEnv("ACCESS_TOKEN_PRIVATE_KEY_PATH", ""),
		AccessTokenPublicKeyPath:  getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ""),
		AccessTokenIssuer:         getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:       getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:          getEnv("ACCESS_TOKEN_KEY_ID", ""),
		AccessTokenTTL:            getEnv("ACCESS_TOKEN_TTL", "15m"),

		SessionTTL:      getEnv("SESSION_TTL", "720h"),
		RefreshTokenTTL: getEnv("REFRESH_TOKEN_TTL", "696h"),

		CleanupInterval:          getEnv("CLEANUP_INTERVAL", "1h"),
		OTPRequestEventRetention: getEnv("OTP_REQUEST_EVENT_RETENTION", "24h"),
		OTPChallengeRetention:    getEnv("OTP_CHALLENGE_RETENTION", "24h"),
		AuthSessionRetention:     getEnv("AUTH_SESSION_RETENTION", "720h"),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
