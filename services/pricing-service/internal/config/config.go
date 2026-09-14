package config

import (
	"os"
	"time"
)

type Config struct {
	ServiceName            string
	Environment            string
	GRPCAddress            string
	MetricsAddress         string
	DatabaseURL            string
	LocationServiceAddress string
	OSRMBaseURL            string
	RoutingTimeout         time.Duration
	WeatherTimeout         time.Duration
}

func Load() Config {
	return Config{
		ServiceName:            getEnv("SERVICE_NAME", "pricing-service"),
		Environment:            getEnv("ENVIRONMENT", "development"),
		GRPCAddress:            getEnv("GRPC_ADDRESS", ":50057"),
		MetricsAddress:         getEnv("METRICS_ADDRESS", ":9097"),
		DatabaseURL:            getEnv("DATABASE_URL", ""),
		LocationServiceAddress: getEnv("LOCATION_SERVICE_ADDRESS", "localhost:50054"),
		OSRMBaseURL:            getEnv("OSRM_BASE_URL", "http://localhost:5000"),
		RoutingTimeout:         3 * time.Second,
		WeatherTimeout:         3 * time.Second,
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
