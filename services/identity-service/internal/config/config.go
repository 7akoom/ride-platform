package config

import "os"

type Config struct {
	ServiceName   string
	Environment   string
	GRPCAddress   string
	DatabaseURL   string
	OTPHashSecret string

	OTPBrandName           string
	OTPProviderHTTPTimeout string

	SMSDefaultProvider string
	SMSRoutes          string

	TelnyxEndpoint           string
	TelnyxAPIKey             string
	TelnyxFrom               string
	TelnyxMessagingProfileID string

	WhatsAppDefaultProvider string
	WhatsAppRoutes          string

	MetaWhatsAppEndpoint           string
	MetaWhatsAppAccessToken        string
	MetaWhatsAppTemplateENName     string
	MetaWhatsAppTemplateENLanguage string
	MetaWhatsAppTemplateARName     string
	MetaWhatsAppTemplateARLanguage string
	MetaWhatsAppTemplateKUName     string
	MetaWhatsAppTemplateKULanguage string

	BulkSMSIraqEndpoint string
	BulkSMSIraqAPIKey   string
	BulkSMSIraqSenderID string

	EmailProvider string

	ResendEndpoint string
	ResendAPIKey   string
	ResendFrom     string

	ValkeyAddress  string
	ValkeyPassword string

	OTPChallengeTTL string

	OTPRequestCooldown    string
	OTPRequestWindow      string
	OTPRequestMaxRequests string

	AccessTokenPrivateKeyPath string
	AccessTokenPublicKeyPath  string
	AccessTokenIssuer         string
	AccessTokenAudience       string
	AccessTokenKeyID          string
	AccessTokenTTL            string

	SessionTTL      string
	RefreshTokenTTL string

	CleanupInterval          string
	OTPRequestEventRetention string
	OTPChallengeRetention    string
	AuthSessionRetention     string
}

func Load() Config {
	return Config{
		ServiceName:   "identity-service",
		Environment:   getEnv("APP_ENV", ""),
		GRPCAddress:   getEnv("GRPC_ADDRESS", ":50051"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		OTPHashSecret: getEnv("OTP_HASH_SECRET", ""),

		OTPBrandName:           getEnv("OTP_BRAND_NAME", "Ride"),
		OTPProviderHTTPTimeout: getEnv("OTP_PROVIDER_HTTP_TIMEOUT", "10s"),

		SMSDefaultProvider: getEnv(
			"SMS_DEFAULT_PROVIDER",
			"",
		),

		SMSRoutes: getEnv(
			"SMS_ROUTES",
			"",
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

		WhatsAppDefaultProvider: getEnv(
			"WHATSAPP_DEFAULT_PROVIDER",
			"",
		),

		WhatsAppRoutes: getEnv(
			"WHATSAPP_ROUTES",
			"",
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

		BulkSMSIraqEndpoint: getEnv("BULKSMSIRAQ_ENDPOINT", ""),
		BulkSMSIraqAPIKey:   getEnv("BULKSMSIRAQ_API_KEY", ""),
		BulkSMSIraqSenderID: getEnv("BULKSMSIRAQ_SENDER_ID", ""),

		EmailProvider: getEnv("EMAIL_PROVIDER", "resend"),

		ResendEndpoint: getEnv(
			"RESEND_ENDPOINT",
			"https://api.resend.com/emails",
		),
		ResendAPIKey: getEnv("RESEND_API_KEY", ""),
		ResendFrom:   getEnv("RESEND_FROM", ""),

		ValkeyAddress:  getEnv("VALKEY_ADDRESS", "127.0.0.1:6380"),
		ValkeyPassword: getEnv("VALKEY_PASSWORD", ""),

		OTPChallengeTTL: getEnv("OTP_CHALLENGE_TTL", "5m"),

		OTPRequestCooldown:    getEnv("OTP_REQUEST_COOLDOWN", "60s"),
		OTPRequestWindow:      getEnv("OTP_REQUEST_WINDOW", "15m"),
		OTPRequestMaxRequests: getEnv("OTP_REQUEST_MAX_REQUESTS", "5"),

		AccessTokenPrivateKeyPath: getEnv("ACCESS_TOKEN_PRIVATE_KEY_PATH", ""),
		AccessTokenPublicKeyPath:  getEnv("ACCESS_TOKEN_PUBLIC_KEY_PATH", ""),
		AccessTokenIssuer:         getEnv("ACCESS_TOKEN_ISSUER", "ride-identity"),
		AccessTokenAudience:       getEnv("ACCESS_TOKEN_AUDIENCE", "ride-platform"),
		AccessTokenKeyID:          getEnv("ACCESS_TOKEN_KEY_ID", ""),
		AccessTokenTTL:            getEnv("ACCESS_TOKEN_TTL", "15m"),

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
