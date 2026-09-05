package config

import "os"

type Config struct {
	ServiceName       string
	Environment       string
	GRPCAddress       string
	MetricsAddress    string
	OTPWebhookAddress string
	DatabaseURL       string
	OTPHashSecret     string

	OTPBrandName           string
	OTPProviderHTTPTimeout string

	SMSDefaultProvider                string
	SMSFallbackProvider               string
	SMSRoutes                         string
	SMSProviderHealthFailureThreshold string
	SMSProviderHealthCooldown         string

	TelnyxEndpoint           string
	TelnyxAPIKey             string
	TelnyxFrom               string
	TelnyxMessagingProfileID string
	TelnyxPublicKey          string

	WhatsAppDefaultProvider                string
	WhatsAppFallbackProvider               string
	WhatsAppRoutes                         string
	WhatsAppProviderHealthFailureThreshold string
	WhatsAppProviderHealthCooldown         string

	MetaWhatsAppEndpoint           string
	MetaWhatsAppAccessToken        string
	MetaWhatsAppTemplateENName     string
	MetaWhatsAppTemplateENLanguage string
	MetaWhatsAppTemplateARName     string
	MetaWhatsAppTemplateARLanguage string
	MetaWhatsAppTemplateKUName     string
	MetaWhatsAppTemplateKULanguage string
	MetaWebhookVerifyToken         string
	MetaAppSecret                  string

	BulkSMSIraqEndpoint    string
	BulkSMSIraqOTPEndpoint string
	BulkSMSIraqAPIKey      string
	BulkSMSIraqSenderID    string

	EmailProvider string

	ResendEndpoint string
	ResendAPIKey   string
	ResendFrom     string

	ValkeyAddress  string
	ValkeyPassword string

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

	OTPChallengeTTL string

	OTPRequestCooldown          string
	OTPRequestWindow            string
	OTPRequestMaxRequests       string
	OTPRequestSourceWindow      string
	OTPRequestSourceMaxRequests string

	AccessTokenPrivateKeyPath   string
	AccessTokenPublicKeyPath    string
	AccessTokenVerificationKeys string
	AccessTokenIssuer           string
	AccessTokenAudience         string
	AccessTokenKeyID            string
	AccessTokenTTL              string

	SessionTTL      string
	RefreshTokenTTL string

	CleanupInterval          string
	OTPRequestEventRetention string
	OTPChallengeRetention    string
	AuthSessionRetention     string
}

func Load() Config {
	return Config{
		ServiceName:    "identity-service",
		Environment:    getEnv("APP_ENV", ""),
		GRPCAddress:    getEnv("GRPC_ADDRESS", ":50051"),
		MetricsAddress: getEnv("METRICS_ADDRESS", ":9090"),
		OTPWebhookAddress: getEnv(
			"OTP_WEBHOOK_ADDRESS",
			":8081",
		),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		OTPHashSecret: getEnv("OTP_HASH_SECRET", ""),

		OTPBrandName:           getEnv("OTP_BRAND_NAME", "Ride"),
		OTPProviderHTTPTimeout: getEnv("OTP_PROVIDER_HTTP_TIMEOUT", "10s"),

		SMSDefaultProvider: getEnv(
			"SMS_DEFAULT_PROVIDER",
			"",
		),

		SMSFallbackProvider: getEnv(
			"SMS_FALLBACK_PROVIDER",
			"",
		),

		SMSRoutes: getEnv(
			"SMS_ROUTES",
			"",
		),

		SMSProviderHealthFailureThreshold: getEnv(
			"SMS_PROVIDER_HEALTH_FAILURE_THRESHOLD",
			"3",
		),

		SMSProviderHealthCooldown: getEnv(
			"SMS_PROVIDER_HEALTH_COOLDOWN",
			"60s",
		),

		TelnyxEndpoint: getEnv(
			"TELNYX_ENDPOINT",
			"https://api.telnyx.com/v2/messages",
		),

		TelnyxAPIKey: getEnv(
			"TELNYX_API_KEY",
			"",
		),

		TelnyxFrom: getEnv(
			"TELNYX_FROM",
			"",
		),

		TelnyxMessagingProfileID: getEnv(
			"TELNYX_MESSAGING_PROFILE_ID",
			"",
		),

		TelnyxPublicKey: getEnv(
			"TELNYX_PUBLIC_KEY",
			"",
		),

		WhatsAppDefaultProvider: getEnv(
			"WHATSAPP_DEFAULT_PROVIDER",
			"",
		),

		WhatsAppRoutes: getEnv(
			"WHATSAPP_ROUTES",
			"",
		),

		WhatsAppFallbackProvider: getEnv(
			"WHATSAPP_FALLBACK_PROVIDER",
			"",
		),

		WhatsAppProviderHealthFailureThreshold: getEnv(
			"WHATSAPP_PROVIDER_HEALTH_FAILURE_THRESHOLD",
			"3",
		),

		WhatsAppProviderHealthCooldown: getEnv(
			"WHATSAPP_PROVIDER_HEALTH_COOLDOWN",
			"60s",
		),

		MetaWhatsAppEndpoint: getEnv(
			"META_WHATSAPP_ENDPOINT",
			"",
		),

		MetaWhatsAppAccessToken: getEnv(
			"META_WHATSAPP_ACCESS_TOKEN",
			"",
		),

		MetaWhatsAppTemplateENName: getEnv(
			"META_WHATSAPP_TEMPLATE_EN_NAME",
			"",
		),

		MetaWhatsAppTemplateENLanguage: getEnv(
			"META_WHATSAPP_TEMPLATE_EN_LANGUAGE",
			"en_US",
		),

		MetaWhatsAppTemplateARName: getEnv(
			"META_WHATSAPP_TEMPLATE_AR_NAME",
			"",
		),

		MetaWhatsAppTemplateARLanguage: getEnv(
			"META_WHATSAPP_TEMPLATE_AR_LANGUAGE",
			"ar",
		),

		MetaWhatsAppTemplateKUName: getEnv(
			"META_WHATSAPP_TEMPLATE_KU_NAME",
			"",
		),

		MetaWhatsAppTemplateKULanguage: getEnv(
			"META_WHATSAPP_TEMPLATE_KU_LANGUAGE",
			"",
		),

		MetaWebhookVerifyToken: getEnv(
			"META_WEBHOOK_VERIFY_TOKEN",
			"",
		),

		MetaAppSecret: getEnv(
			"META_APP_SECRET",
			"",
		),

		BulkSMSIraqEndpoint: getEnv(
			"BULKSMSIRAQ_ENDPOINT",
			"",
		),

		BulkSMSIraqOTPEndpoint: getEnv(
			"BULKSMSIRAQ_OTP_ENDPOINT",
			"",
		),

		BulkSMSIraqAPIKey: getEnv(
			"BULKSMSIRAQ_API_KEY",
			"",
		),

		BulkSMSIraqSenderID: getEnv(
			"BULKSMSIRAQ_SENDER_ID",
			"",
		),

		EmailProvider: getEnv("EMAIL_PROVIDER", "resend"),

		ResendEndpoint: getEnv(
			"RESEND_ENDPOINT",
			"https://api.resend.com/emails",
		),
		ResendAPIKey: getEnv("RESEND_API_KEY", ""),
		ResendFrom:   getEnv("RESEND_FROM", ""),

		ValkeyAddress:  getEnv("VALKEY_ADDRESS", "127.0.0.1:6380"),
		ValkeyPassword: getEnv("VALKEY_PASSWORD", ""),

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

		OTPChallengeTTL: getEnv("OTP_CHALLENGE_TTL", "5m"),

		OTPRequestCooldown:          getEnv("OTP_REQUEST_COOLDOWN", "60s"),
		OTPRequestWindow:            getEnv("OTP_REQUEST_WINDOW", "15m"),
		OTPRequestMaxRequests:       getEnv("OTP_REQUEST_MAX_REQUESTS", "5"),
		OTPRequestSourceWindow:      getEnv("OTP_REQUEST_SOURCE_WINDOW", "10m"),
		OTPRequestSourceMaxRequests: getEnv("OTP_REQUEST_SOURCE_MAX_REQUESTS", "30"),

		AccessTokenPrivateKeyPath: getEnv(
			"ACCESS_TOKEN_PRIVATE_KEY_PATH",
			"",
		),
		AccessTokenPublicKeyPath: getEnv(
			"ACCESS_TOKEN_PUBLIC_KEY_PATH",
			"",
		),
		AccessTokenVerificationKeys: getEnv(
			"ACCESS_TOKEN_VERIFICATION_KEYS",
			"",
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
			"",
		),
		AccessTokenTTL: getEnv(
			"ACCESS_TOKEN_TTL",
			"15m",
		),

		SessionTTL:      getEnv("SESSION_TTL", "720h"),
		RefreshTokenTTL: getEnv("REFRESH_TOKEN_TTL", "696h"),

		CleanupInterval:          getEnv("CLEANUP_INTERVAL", "1h"),
		OTPRequestEventRetention: getEnv("OTP_REQUEST_EVENT_RETENTION", "24h"),
		OTPChallengeRetention:    getEnv("OTP_CHALLENGE_RETENTION", "24h"),
		AuthSessionRetention:     getEnv("AUTH_SESSION_RETENTION", "720h"),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
