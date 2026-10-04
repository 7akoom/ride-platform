package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

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

	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	// The services the support desk reads from and acts through, all called
	// as a service (internal token).
	StaffServiceAddress    string
	MediaServiceAddress    string
	TripServiceAddress     string
	RiderServiceAddress    string
	DriverServiceAddress   string
	WalletServiceAddress   string
	IdentityServiceAddress string

	// Money one ticket may move (refunds, fee waivers, wallet credits)
	// before a second staff member with support.approve has to agree.
	RefundLimit string
	// Open tickets one person may have per audience.
	MaxOpenTickets string
	// How old a trip may be for a ticket about it.
	TripMaxAge string
	// How often actions left processing are retried, due suspensions lifted
	// and stale tickets moved on.
	WorkerInterval string
	// First-response targets: "urgent=15m,high=1h,normal=4h,low=24h".
	FirstResponseTargets string
	// A ticket waiting for the person is resolved after this; a resolved one
	// is closed after this. 0: never.
	AutoResolveAfter string
	AutoCloseAfter   string

	InternalServiceToken string

	RateLimitRequestsPerSecond string
	RateLimitBurst             string
}

func Load() Config {
	return Config{
		ServiceName:    getEnv("SERVICE_NAME", "support-service"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50063"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9103"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),

		NATSURL:            getEnv("NATS_URL", "nats://127.0.0.1:4222"),
		NATSPublishTimeout: getEnv("NATS_PUBLISH_TIMEOUT", "2s"),
		NATSConnectTimeout: getEnv("NATS_CONNECT_TIMEOUT", "5s"),
		NATSReconnectWait:  getEnv("NATS_RECONNECT_WAIT", "2s"),
		NATSDrainTimeout:   getEnv("NATS_DRAIN_TIMEOUT", "10s"),

		OutboxPollInterval:      getEnv("OUTBOX_POLL_INTERVAL", "500ms"),
		OutboxLeaseDuration:     getEnv("OUTBOX_LEASE_DURATION", "30s"),
		OutboxBatchSize:         getEnv("OUTBOX_BATCH_SIZE", "10"),
		OutboxInitialRetryDelay: getEnv("OUTBOX_INITIAL_RETRY_DELAY", "1s"),
		OutboxMaxRetryDelay:     getEnv("OUTBOX_MAX_RETRY_DELAY", "1m"),

		AccessTokenPublicKeyPath: getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ".local/keys/access_token_public.pem"),
		AccessTokenIssuer:        getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:      getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:         getEnv("ACCESS_TOKEN_KEY_ID", "identity-dev-1"),

		StaffServiceAddress:    getEnv("STAFF_SERVICE_ADDRESS", "localhost:50061"),
		MediaServiceAddress:    getEnv("MEDIA_SERVICE_ADDRESS", "localhost:50062"),
		TripServiceAddress:     getEnv("TRIP_SERVICE_ADDRESS", "localhost:50055"),
		RiderServiceAddress:    getEnv("RIDER_SERVICE_ADDRESS", "localhost:50052"),
		DriverServiceAddress:   getEnv("DRIVER_SERVICE_ADDRESS", "localhost:50053"),
		WalletServiceAddress:   getEnv("WALLET_SERVICE_ADDRESS", "localhost:50058"),
		IdentityServiceAddress: getEnv("IDENTITY_SERVICE_ADDRESS", "localhost:50051"),

		RefundLimit:    getEnv("SUPPORT_REFUND_LIMIT", "25000"),
		MaxOpenTickets: getEnv("SUPPORT_MAX_OPEN_TICKETS", "10"),
		TripMaxAge:     getEnv("SUPPORT_TRIP_MAX_AGE", "720h"),
		WorkerInterval: getEnv("SUPPORT_WORKER_INTERVAL", "15s"),

		FirstResponseTargets: getEnv("SUPPORT_FIRST_RESPONSE", "urgent=15m,high=1h,normal=4h,low=24h"),
		AutoResolveAfter:     getEnv("SUPPORT_AUTO_RESOLVE_AFTER", "72h"),
		AutoCloseAfter:       getEnv("SUPPORT_AUTO_CLOSE_AFTER", "168h"),

		InternalServiceToken: getEnv("INTERNAL_SERVICE_TOKEN", "dev-internal-service-token-change-me"),

		RateLimitRequestsPerSecond: getEnv("RATE_LIMIT_REQUESTS_PER_SECOND", "20"),
		RateLimitBurst:             getEnv("RATE_LIMIT_BURST", "40"),
	}
}

// Desk is the parsed support desk settings.
type Desk struct {
	RefundLimit    decimal.Decimal
	MaxOpenTickets int
	TripMaxAge     time.Duration
	WorkerInterval time.Duration

	FirstResponse    map[string]time.Duration
	AutoResolveAfter time.Duration
	AutoCloseAfter   time.Duration
}

func ParseDesk(cfg Config) (Desk, error) {
	limit, err := decimal.NewFromString(strings.TrimSpace(cfg.RefundLimit))
	if err != nil || limit.IsNegative() {
		return Desk{}, fmt.Errorf("SUPPORT_REFUND_LIMIT must be a number of at least 0, got %q", cfg.RefundLimit)
	}

	maxOpen, err := strconv.Atoi(strings.TrimSpace(cfg.MaxOpenTickets))
	if err != nil || maxOpen <= 0 || maxOpen > 1000 {
		return Desk{}, fmt.Errorf("SUPPORT_MAX_OPEN_TICKETS must be 1-1000, got %q", cfg.MaxOpenTickets)
	}

	tripMaxAge, err := parsePositiveDuration("SUPPORT_TRIP_MAX_AGE", cfg.TripMaxAge)
	if err != nil {
		return Desk{}, err
	}

	interval, err := parsePositiveDuration("SUPPORT_WORKER_INTERVAL", cfg.WorkerInterval)
	if err != nil {
		return Desk{}, err
	}

	targets := map[string]time.Duration{}

	for _, part := range strings.Split(cfg.FirstResponseTargets, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		name, value, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)

		switch name {
		case "urgent", "high", "normal", "low":
		default:
			ok = false
		}

		d, err := time.ParseDuration(strings.TrimSpace(value))
		if !ok || err != nil || d <= 0 || d > 30*24*time.Hour {
			return Desk{}, fmt.Errorf("SUPPORT_FIRST_RESPONSE is priority=duration pairs (urgent, high, normal, low), got %q", part)
		}

		targets[name] = d
	}

	autoResolve, err := parseOptionalDuration("SUPPORT_AUTO_RESOLVE_AFTER", cfg.AutoResolveAfter)
	if err != nil {
		return Desk{}, err
	}

	autoClose, err := parseOptionalDuration("SUPPORT_AUTO_CLOSE_AFTER", cfg.AutoCloseAfter)
	if err != nil {
		return Desk{}, err
	}

	return Desk{
		RefundLimit:      limit,
		MaxOpenTickets:   maxOpen,
		TripMaxAge:       tripMaxAge,
		WorkerInterval:   interval,
		FirstResponse:    targets,
		AutoResolveAfter: autoResolve,
		AutoCloseAfter:   autoClose,
	}, nil
}

// parseOptionalDuration: 0 switches the feature off.
func parseOptionalDuration(name, value string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%s must be a duration (0 to switch it off), got %q", name, value)
	}

	return d, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
