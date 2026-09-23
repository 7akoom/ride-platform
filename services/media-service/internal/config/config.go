package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	DatabaseURL    string

	// The S3-compatible object store (SeaweedFS). S3Endpoint is how this
	// service reaches it; S3PublicEndpoint is the address the apps reach and
	// that presigned URLs carry.
	S3Endpoint       string
	S3PublicEndpoint string
	S3Region         string
	S3Bucket         string
	S3AccessKey      string
	S3SecretKey      string

	UploadURLTTL             string
	DownloadURLTTL           string
	PendingUploadTTL         string
	MaxPendingPerOwner       string
	MaxConcurrentInspections string

	// staff-service, asked before a staff member reads someone else's file.
	StaffServiceAddress string

	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	InternalServiceToken string

	RateLimitRequestsPerSecond string
	RateLimitBurst             string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "media-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50062"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9102"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),

		S3Endpoint:       getEnv("S3_ENDPOINT", "http://localhost:8333"),
		S3PublicEndpoint: getEnv("S3_PUBLIC_ENDPOINT", "http://127.0.0.1:8333"),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3Bucket:         getEnv("S3_BUCKET", "ride-media"),
		S3AccessKey:      getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:      getEnv("S3_SECRET_KEY", ""),

		UploadURLTTL:             getEnv("UPLOAD_URL_TTL", "10m"),
		DownloadURLTTL:           getEnv("DOWNLOAD_URL_TTL", "5m"),
		PendingUploadTTL:         getEnv("PENDING_UPLOAD_TTL", "30m"),
		MaxPendingPerOwner:       getEnv("MAX_PENDING_UPLOADS_PER_OWNER", "20"),
		MaxConcurrentInspections: getEnv("MAX_CONCURRENT_INSPECTIONS", "2"),

		StaffServiceAddress: getEnv("STAFF_SERVICE_ADDRESS", "localhost:50061"),

		AccessTokenPublicKeyPath: getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ".local/keys/access_token_public.pem"),
		AccessTokenIssuer:        getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:      getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:         getEnv("ACCESS_TOKEN_KEY_ID", "identity-dev-1"),

		InternalServiceToken: getEnv("INTERNAL_SERVICE_TOKEN", "dev-internal-service-token-change-me"),

		RateLimitRequestsPerSecond: getEnv("RATE_LIMIT_REQUESTS_PER_SECOND", "20"),
		RateLimitBurst:             getEnv("RATE_LIMIT_BURST", "40"),
	}
}

// Limits are the parsed timings and limits.
type Limits struct {
	UploadURLTTL             time.Duration
	DownloadURLTTL           time.Duration
	PendingUploadTTL         time.Duration
	MaxPendingPerOwner       int
	MaxConcurrentInspections int
}

func ParseLimits(cfg Config) (Limits, error) {
	var limits Limits

	durations := []struct {
		name  string
		value string
		out   *time.Duration
	}{
		{"UPLOAD_URL_TTL", cfg.UploadURLTTL, &limits.UploadURLTTL},
		{"DOWNLOAD_URL_TTL", cfg.DownloadURLTTL, &limits.DownloadURLTTL},
		{"PENDING_UPLOAD_TTL", cfg.PendingUploadTTL, &limits.PendingUploadTTL},
	}

	for _, d := range durations {
		parsed, err := time.ParseDuration(strings.TrimSpace(d.value))
		if err != nil || parsed <= 0 {
			return Limits{}, fmt.Errorf("%s must be a positive duration, got %q", d.name, d.value)
		}

		*d.out = parsed
	}

	if limits.UploadURLTTL > 7*24*time.Hour || limits.DownloadURLTTL > 7*24*time.Hour {
		return Limits{}, fmt.Errorf("presigned URLs cannot live longer than 7 days")
	}

	if limits.PendingUploadTTL <= limits.UploadURLTTL {
		return Limits{}, fmt.Errorf("PENDING_UPLOAD_TTL must be longer than UPLOAD_URL_TTL")
	}

	integers := []struct {
		name  string
		value string
		out   *int
	}{
		{"MAX_PENDING_UPLOADS_PER_OWNER", cfg.MaxPendingPerOwner, &limits.MaxPendingPerOwner},
		{"MAX_CONCURRENT_INSPECTIONS", cfg.MaxConcurrentInspections, &limits.MaxConcurrentInspections},
	}

	for _, i := range integers {
		parsed, err := strconv.Atoi(strings.TrimSpace(i.value))
		if err != nil || parsed <= 0 {
			return Limits{}, fmt.Errorf("%s must be a positive integer, got %q", i.name, i.value)
		}

		*i.out = parsed
	}

	return limits, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
