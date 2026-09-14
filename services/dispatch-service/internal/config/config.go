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
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
