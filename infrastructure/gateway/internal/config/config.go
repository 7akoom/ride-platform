package config

import "os"

type Config struct {
	ServiceName string
	Environment string

	// Address this gateway listens on for incoming client HTTP/JSON
	// requests (Rider/Driver apps, Admin web). TLS termination happens
	// in front of this process (e.g. Nginx on the VPS) — this listens
	// plain HTTP, matching how every backend service already listens
	// plain gRPC internally.
	HTTPAddress string

	// Backend gRPC targets this gateway proxies to. One field per
	// service, added as its client-facing RPCs get `google.api.http`
	// annotations.
	TripServiceAddress         string
	WalletServiceAddress       string
	LocationServiceAddress     string
	RiderServiceAddress        string
	DriverServiceAddress       string
	PricingServiceAddress      string
	NotificationServiceAddress string
	IdentityServiceAddress     string
	StaffServiceAddress        string

	// Comma-separated browser origins allowed to call this gateway
	// (the Admin web app). Mobile apps don't send an Origin header and
	// are unaffected either way.
	AllowedOrigins string
}

func Load() Config {
	return Config{
		ServiceName: getEnv("SERVICE_NAME", "gateway"),
		Environment: getEnv("ENVIRONMENT", "development"),
		HTTPAddress: getEnv("HTTP_ADDRESS", ":8080"),

		TripServiceAddress: getEnv("TRIP_SERVICE_ADDRESS", "localhost:50055"),
		WalletServiceAddress: getEnv(
			"WALLET_SERVICE_ADDRESS",
			"localhost:50058",
		),
		LocationServiceAddress: getEnv(
			"LOCATION_SERVICE_ADDRESS",
			"localhost:50054",
		),
		RiderServiceAddress: getEnv(
			"RIDER_SERVICE_ADDRESS",
			"localhost:50052",
		),
		DriverServiceAddress: getEnv(
			"DRIVER_SERVICE_ADDRESS",
			"localhost:50053",
		),
		PricingServiceAddress: getEnv(
			"PRICING_SERVICE_ADDRESS",
			"localhost:50057",
		),
		NotificationServiceAddress: getEnv(
			"NOTIFICATION_SERVICE_ADDRESS",
			"localhost:50059",
		),
		IdentityServiceAddress: getEnv(
			"IDENTITY_SERVICE_ADDRESS",
			"localhost:50051",
		),
		StaffServiceAddress: getEnv(
			"STAFF_SERVICE_ADDRESS",
			"localhost:50061",
		),

		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "http://localhost:3000"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
