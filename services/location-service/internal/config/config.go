package config

import "os"

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	ValkeyAddress  string
	ValkeyPassword string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "location-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50054"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9094"),
		ValkeyAddress:  getEnv("VALKEY_ADDRESS", ""),
		ValkeyPassword: getEnv("VALKEY_PASSWORD", ""),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
