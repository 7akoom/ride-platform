package config

import "os"

type Config struct {
	ServiceName          string
	Environment          string
	GRPCAddress          string
	MetricsAddress       string
	DatabaseURL          string
	TripServiceAddress   string
	RiderServiceAddress  string
	DriverServiceAddress string
	// IdentityServiceAddress is where wallet PINs are checked and riders are
	// found by phone (transfers).
	IdentityServiceAddress string
	// StaffServiceAddress is asked, for every voucher admin call, whether the
	// staff member holds vouchers.manage (it audits the call).
	StaffServiceAddress string

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

	// Auto-settlement: this service consumes fare.calculated and settles
	// the trip by itself (see config/auto_settle.go).
	SettlementRetryInterval        string
	SettlementGiveUpAfter          string
	SettlementDefaultPaymentMethod string

	// Verifies access tokens issued by identity-service. The public key
	// must be copied from identity-service's own .local/keys directory —
	// it is never committed to the repo.
	AccessTokenPublicKeyPath string
	AccessTokenIssuer        string
	AccessTokenAudience      string
	AccessTokenKeyID         string

	// Shared secret for service-to-service calls (e.g. a background NATS
	// consumer or another service acting on its own behalf, where there is
	// no end-user access token to check). Must be identical across every
	// service in a deployment; treat it like a password — the checked-in
	// default is for local dev only.
	InternalServiceToken string

	// Per-caller token-bucket rate limit (see
	// transport/grpc/rate_limit_interceptor.go). Defaults are generous
	// for a single mobile-app client under normal use, tight enough to
	// stop a runaway retry loop or a naive script.
	RateLimitRequestsPerSecond string
	RateLimitBurst             string

	// ZainCash Payment Gateway v2 (driver wallet top-ups). BaseURL/
	// ClientID/ClientSecret/Scope come from ZainCash onboarding.
	// WebhookSecret verifies the HS256 JWT ZainCash sends on both the
	// webhook and the redirect callback — confirm at onboarding whether
	// it's the same value as ClientSecret or a separate one.
	ZainCashBaseURL       string
	ZainCashClientID      string
	ZainCashClientSecret  string
	ZainCashScope         string
	ZainCashWebhookSecret string

	// Where ZainCash redirects the driver's browser after payment.
	// Placeholder pages until the driver app exists.
	ZainCashSuccessURL string
	ZainCashFailureURL string

	// Vouchers (see config/vouchers.go). VoucherCodeKey hashes and seals the
	// codes; keep it secret and never change it once vouchers are issued.
	VoucherCodeKey           string
	VoucherRedeemMaxFailures string
	VoucherRedeemWindow      string
}

func Load() Config {
	return Config{
		ServiceName:          getEnv("SERVICE_NAME", "wallet-service"),
		Environment:          getEnv("ENVIRONMENT", "development"),
		GRPCAddress:          getEnv("GRPC_ADDRESS", ":50058"),
		MetricsAddress:       getEnv("METRICS_ADDRESS", ":9098"),
		DatabaseURL:          getEnv("DATABASE_URL", ""),
		TripServiceAddress:   getEnv("TRIP_SERVICE_ADDRESS", "localhost:50055"),
		RiderServiceAddress:  getEnv("RIDER_SERVICE_ADDRESS", "localhost:50052"),
		DriverServiceAddress: getEnv("DRIVER_SERVICE_ADDRESS", "localhost:50053"),

		IdentityServiceAddress: getEnv("IDENTITY_SERVICE_ADDRESS", "localhost:50051"),
		StaffServiceAddress:    getEnv("STAFF_SERVICE_ADDRESS", "localhost:50061"),

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

		SettlementRetryInterval: getEnv(
			"SETTLEMENT_RETRY_INTERVAL",
			"10s",
		),

		SettlementGiveUpAfter: getEnv(
			"SETTLEMENT_GIVE_UP_AFTER",
			"30m",
		),

		SettlementDefaultPaymentMethod: getEnv(
			"SETTLEMENT_DEFAULT_PAYMENT_METHOD",
			"cash",
		),

		AccessTokenPublicKeyPath: getEnv(
			"ACCESS_TOKEN_PUBLIC_KEY_PATH",
			".local/keys/access_token_public.pem",
		),

		AccessTokenIssuer: getEnv(
			"ACCESS_TOKEN_ISSUER",
			"ride-identity",
		),

		AccessTokenAudience: getEnv(
			"ACCESS_TOKEN_AUDIENCE",
			"ride-platform",
		),

		AccessTokenKeyID: getEnv(
			"ACCESS_TOKEN_KEY_ID",
			"identity-dev-1",
		),

		InternalServiceToken: getEnv(
			"INTERNAL_SERVICE_TOKEN",
			"dev-internal-service-token-change-me",
		),

		RateLimitRequestsPerSecond: getEnv(
			"RATE_LIMIT_REQUESTS_PER_SECOND",
			"20",
		),

		RateLimitBurst: getEnv(
			"RATE_LIMIT_BURST",
			"40",
		),

		ZainCashBaseURL: getEnv(
			"ZAINCASH_BASE_URL",
			"https://test.zaincash.iq",
		),

		ZainCashClientID: getEnv("ZAINCASH_CLIENT_ID", ""),

		ZainCashClientSecret: getEnv("ZAINCASH_CLIENT_SECRET", ""),

		ZainCashScope: getEnv(
			"ZAINCASH_SCOPE",
			"payment:read payment:write",
		),

		ZainCashWebhookSecret: getEnv("ZAINCASH_WEBHOOK_SECRET", ""),

		ZainCashSuccessURL: getEnv(
			"ZAINCASH_SUCCESS_URL",
			"https://example.com/wallet/topup/success",
		),

		ZainCashFailureURL: getEnv(
			"ZAINCASH_FAILURE_URL",
			"https://example.com/wallet/topup/failure",
		),

		VoucherCodeKey:           getEnv("VOUCHER_CODE_KEY", developmentVoucherCodeKey),
		VoucherRedeemMaxFailures: getEnv("VOUCHER_REDEEM_MAX_FAILURES", "5"),
		VoucherRedeemWindow:      getEnv("VOUCHER_REDEEM_WINDOW", "1h"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
