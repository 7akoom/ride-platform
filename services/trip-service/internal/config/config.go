package config

import "os"

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	DatabaseURL    string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "trip-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50055"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9095"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
