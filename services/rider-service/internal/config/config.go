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
		ServiceName:    getEnv("SERVICE_NAME", "rider-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50052"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9092"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
