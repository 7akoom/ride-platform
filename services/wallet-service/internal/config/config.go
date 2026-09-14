package config

import "os"

type Config struct {
	ServiceName    string
	Environment    string
	GRPCAddress    string
	MetricsAddress string
	DatabaseURL    string

	NATSURL            string
	NATSPublishTimeout string
	NATSConnectTimeout string
	NATSReconnectWait  string
	NATSDrainTimeout   string

	OutboxPollInterval      string
	OutboxLeaseDuration     string
	OutboxBatchSize         string
	OutboxInitialRetryDelay string
	OutboxMaxRetryDelay     string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "wallet-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50058"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9098"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),

		NATSURL: getEnv(
			"NATS_URL",
			"nats://127.0.0.1:4222",
		),

		NATSPublishTimeout: getEnv(
			"NATS_PUBLISH_TIMEOUT",
			"2s",
		),

		NATSConnectTimeout: getEnv(
			"NATS_CONNECT_TIMEOUT",
			"5s",
		),

		NATSReconnectWait: getEnv(
			"NATS_RECONNECT_WAIT",
			"2s",
		),

		NATSDrainTimeout: getEnv(
			"NATS_DRAIN_TIMEOUT",
			"10s",
		),

		OutboxPollInterval: getEnv(
			"OUTBOX_POLL_INTERVAL",
			"500ms",
		),

		OutboxLeaseDuration: getEnv(
			"OUTBOX_LEASE_DURATION",
			"30s",
		),

		OutboxBatchSize: getEnv(
			"OUTBOX_BATCH_SIZE",
			"10",
		),

		OutboxInitialRetryDelay: getEnv(
			"OUTBOX_INITIAL_RETRY_DELAY",
			"1s",
		),

		OutboxMaxRetryDelay: getEnv(
			"OUTBOX_MAX_RETRY_DELAY",
			"1m",
		),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
