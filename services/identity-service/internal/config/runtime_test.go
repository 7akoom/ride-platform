package config

import (
	"testing"
	"time"
)

func TestValidateRuntimeAcceptsValidConfiguration(
	t *testing.T,
) {
	natsConfig := NATS{
		PublishTimeout: 2 * time.Second,
	}

	outboxConfig := Outbox{
		LeaseDuration: 30 * time.Second,
		BatchSize:     10,
	}

	if err := ValidateRuntime(
		natsConfig,
		outboxConfig,
	); err != nil {
		t.Fatalf(
			"ValidateRuntime() returned an error: %v",
			err,
		)
	}
}

func TestValidateRuntimeRejectsPublishBudgetEqualToLease(
	t *testing.T,
) {
	natsConfig := NATS{
		PublishTimeout: 3 * time.Second,
	}

	outboxConfig := Outbox{
		LeaseDuration: 30 * time.Second,
		BatchSize:     10,
	}

	err := ValidateRuntime(
		natsConfig,
		outboxConfig,
	)

	if err == nil {
		t.Fatal(
			"ValidateRuntime() allowed publish budget equal to lease duration",
		)
	}
}

func TestValidateRuntimeRejectsPublishBudgetGreaterThanLease(
	t *testing.T,
) {
	natsConfig := NATS{
		PublishTimeout: 4 * time.Second,
	}

	outboxConfig := Outbox{
		LeaseDuration: 30 * time.Second,
		BatchSize:     10,
	}

	err := ValidateRuntime(
		natsConfig,
		outboxConfig,
	)

	if err == nil {
		t.Fatal(
			"ValidateRuntime() allowed publish budget greater than lease duration",
		)
	}
}

func TestValidateRuntimeRejectsNonPositivePublishTimeout(
	t *testing.T,
) {
	tests := []time.Duration{
		0,
		-time.Second,
	}

	for _, value := range tests {
		t.Run(
			value.String(),
			func(t *testing.T) {
				err := ValidateRuntime(
					NATS{
						PublishTimeout: value,
					},
					Outbox{
						LeaseDuration: 30 * time.Second,
						BatchSize:     10,
					},
				)

				if err == nil {
					t.Fatalf(
						"ValidateRuntime() accepted PublishTimeout=%v",
						value,
					)
				}
			},
		)
	}
}

func TestValidateRuntimeRejectsNonPositiveLeaseDuration(
	t *testing.T,
) {
	tests := []time.Duration{
		0,
		-time.Second,
	}

	for _, value := range tests {
		t.Run(
			value.String(),
			func(t *testing.T) {
				err := ValidateRuntime(
					NATS{
						PublishTimeout: time.Second,
					},
					Outbox{
						LeaseDuration: value,
						BatchSize:     10,
					},
				)

				if err == nil {
					t.Fatalf(
						"ValidateRuntime() accepted LeaseDuration=%v",
						value,
					)
				}
			},
		)
	}
}

func TestValidateRuntimeRejectsNonPositiveBatchSize(
	t *testing.T,
) {
	tests := []int{
		0,
		-1,
	}

	for _, value := range tests {
		t.Run(
			func() string {
				if value == 0 {
					return "zero"
				}

				return "negative"
			}(),
			func(t *testing.T) {
				err := ValidateRuntime(
					NATS{
						PublishTimeout: time.Second,
					},
					Outbox{
						LeaseDuration: 30 * time.Second,
						BatchSize:     value,
					},
				)

				if err == nil {
					t.Fatalf(
						"ValidateRuntime() accepted BatchSize=%d",
						value,
					)
				}
			},
		)
	}
}

func TestMaxPublishTimeoutForLeaseReturnsExpectedBoundary(
	t *testing.T,
) {
	tests := []struct {
		name     string
		lease    time.Duration
		batch    int
		expected time.Duration
	}{
		{
			name:     "thirty seconds divided across ten events",
			lease:    30 * time.Second,
			batch:    10,
			expected: 3*time.Second - 1,
		},
		{
			name:     "one second single event",
			lease:    time.Second,
			batch:    1,
			expected: time.Second - 1,
		},
		{
			name:     "zero lease",
			lease:    0,
			batch:    10,
			expected: 0,
		},
		{
			name:     "zero batch",
			lease:    30 * time.Second,
			batch:    0,
			expected: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				actual :=
					maxPublishTimeoutForLease(
						testCase.lease,
						testCase.batch,
					)

				if actual != testCase.expected {
					t.Fatalf(
						"maxPublishTimeoutForLease() = %v, expected %v",
						actual,
						testCase.expected,
					)
				}
			},
		)
	}
}

func TestDefaultRuntimeConfigurationIsValid(
	t *testing.T,
) {
	cfg := Load()

	natsConfig, err := ParseNATS(cfg)
	if err != nil {
		t.Fatalf(
			"ParseNATS() returned an error: %v",
			err,
		)
	}

	outboxConfig, err := ParseOutbox(cfg)
	if err != nil {
		t.Fatalf(
			"ParseOutbox() returned an error: %v",
			err,
		)
	}

	if err := ValidateRuntime(
		natsConfig,
		outboxConfig,
	); err != nil {
		t.Fatalf(
			"default runtime configuration is invalid: %v",
			err,
		)
	}
}
func TestValidateProductionProvidersSkipsNonProductionEnvironment(
	t *testing.T,
) {
	environments := []string{
		"development",
		"test",
		"Development",
		" TEST ",
	}

	for _, environment := range environments {
		t.Run(
			environment,
			func(t *testing.T) {
				err := ValidateProductionProviders(
					Config{
						Environment: environment,

						SMSDefaultProvider: "unsupported-provider",

						WhatsAppDefaultProvider: "unsupported-provider",
					},
				)

				if err != nil {
					t.Fatalf(
						"ValidateProductionProviders() returned an error outside production: %v",
						err,
					)
				}
			},
		)
	}
}

func TestValidateProductionProvidersAcceptsBulkSMSIraqSMS(
	t *testing.T,
) {
	cfg := Config{
		Environment: "production",

		SMSDefaultProvider: "bulksmsiraq",

		BulkSMSIraqOTPEndpoint: "https://example.com/api/v5/otp/send",

		BulkSMSIraqAPIKey: "test-api-key",

		BulkSMSIraqSenderID: "sender",
	}

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() returned an error: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersRejectsIncompleteBulkSMSIraqSMS(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "missing OTP endpoint",
			mutate: func(cfg *Config) {
				cfg.BulkSMSIraqOTPEndpoint = ""
			},
		},
		{
			name: "missing API key",
			mutate: func(cfg *Config) {
				cfg.BulkSMSIraqAPIKey = ""
			},
		},
		{
			name: "missing sender ID",
			mutate: func(cfg *Config) {
				cfg.BulkSMSIraqSenderID = ""
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				cfg := Config{
					Environment: "production",

					SMSDefaultProvider: "bulksmsiraq",

					BulkSMSIraqOTPEndpoint: "https://example.com/api/v5/otp/send",

					BulkSMSIraqAPIKey: "test-api-key",

					BulkSMSIraqSenderID: "sender",
				}

				testCase.mutate(
					&cfg,
				)

				if err := ValidateProductionProviders(
					cfg,
				); err == nil {
					t.Fatal(
						"ValidateProductionProviders() accepted incomplete BulkSMSIraq SMS configuration",
					)
				}
			},
		)
	}
}

func TestValidateProductionProvidersAcceptsTelnyxSMSWithFrom(
	t *testing.T,
) {
	cfg := Config{
		Environment: "production",

		SMSDefaultProvider: "telnyx",

		TelnyxEndpoint: "https://api.telnyx.com/v2/messages",

		TelnyxAPIKey: "test-api-key",

		TelnyxFrom: "+15555550123",
	}

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() returned an error: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersAcceptsTelnyxSMSWithMessagingProfile(
	t *testing.T,
) {
	cfg := Config{
		Environment: "production",

		SMSDefaultProvider: "telnyx",

		TelnyxEndpoint: "https://api.telnyx.com/v2/messages",

		TelnyxAPIKey: "test-api-key",

		TelnyxMessagingProfileID: "profile-123",
	}

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() returned an error: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersRejectsIncompleteTelnyxSMS(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "missing endpoint",
			mutate: func(cfg *Config) {
				cfg.TelnyxEndpoint = ""
			},
		},
		{
			name: "missing API key",
			mutate: func(cfg *Config) {
				cfg.TelnyxAPIKey = ""
			},
		},
		{
			name: "missing sender and messaging profile",
			mutate: func(cfg *Config) {
				cfg.TelnyxFrom = ""
				cfg.TelnyxMessagingProfileID = ""
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				cfg := Config{
					Environment: "production",

					SMSDefaultProvider: "telnyx",

					TelnyxEndpoint: "https://api.telnyx.com/v2/messages",

					TelnyxAPIKey: "test-api-key",

					TelnyxFrom: "+15555550123",
				}

				testCase.mutate(
					&cfg,
				)

				if err := ValidateProductionProviders(
					cfg,
				); err == nil {
					t.Fatal(
						"ValidateProductionProviders() accepted incomplete Telnyx configuration",
					)
				}
			},
		)
	}
}

func TestValidateProductionProvidersAcceptsBulkSMSIraqWhatsApp(
	t *testing.T,
) {
	cfg := Config{
		Environment: "production",

		WhatsAppDefaultProvider: "bulksmsiraq",

		BulkSMSIraqOTPEndpoint: "https://example.com/api/v5/otp/send",

		BulkSMSIraqAPIKey: "test-api-key",

		BulkSMSIraqSenderID: "sender",
	}

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() returned an error: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersAcceptsMetaWhatsApp(
	t *testing.T,
) {
	cfg := validMetaProductionConfig()

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() returned an error: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersRejectsIncompleteMetaWhatsApp(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "missing endpoint",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppEndpoint = ""
			},
		},
		{
			name: "missing access token",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppAccessToken = ""
			},
		},
		{
			name: "missing English template",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppTemplateENName = ""
			},
		},
		{
			name: "missing English language",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppTemplateENLanguage = ""
			},
		},
		{
			name: "missing Arabic template",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppTemplateARName = ""
			},
		},
		{
			name: "missing Arabic language",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppTemplateARLanguage = ""
			},
		},
		{
			name: "missing Kurdish template",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppTemplateKUName = ""
			},
		},
		{
			name: "missing Kurdish language",
			mutate: func(cfg *Config) {
				cfg.MetaWhatsAppTemplateKULanguage = ""
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				cfg :=
					validMetaProductionConfig()

				testCase.mutate(
					&cfg,
				)

				if err := ValidateProductionProviders(
					cfg,
				); err == nil {
					t.Fatal(
						"ValidateProductionProviders() accepted incomplete Meta WhatsApp configuration",
					)
				}
			},
		)
	}
}

func TestValidateProductionProvidersRejectsSameSMSDefaultAndFallback(
	t *testing.T,
) {
	cfg := Config{
		Environment: "production",

		SMSDefaultProvider: " TELNYX ",

		SMSFallbackProvider: "telnyx",

		TelnyxEndpoint: "https://api.telnyx.com/v2/messages",

		TelnyxAPIKey: "test-api-key",

		TelnyxFrom: "+15555550123",
	}

	if err := ValidateProductionProviders(
		cfg,
	); err == nil {
		t.Fatal(
			"ValidateProductionProviders() accepted identical SMS default and fallback providers",
		)
	}
}

func TestValidateProductionProvidersRejectsSameWhatsAppDefaultAndFallback(
	t *testing.T,
) {
	cfg := validMetaProductionConfig()

	cfg.WhatsAppFallbackProvider =
		" META "

	if err := ValidateProductionProviders(
		cfg,
	); err == nil {
		t.Fatal(
			"ValidateProductionProviders() accepted identical WhatsApp default and fallback providers",
		)
	}
}

func TestValidateProductionProvidersRejectsUnsupportedSMSProvider(
	t *testing.T,
) {
	err := ValidateProductionProviders(
		Config{
			Environment: "production",

			SMSDefaultProvider: "unknown",
		},
	)

	if err == nil {
		t.Fatal(
			"ValidateProductionProviders() accepted unsupported SMS provider",
		)
	}
}

func TestValidateProductionProvidersRejectsUnsupportedWhatsAppProvider(
	t *testing.T,
) {
	err := ValidateProductionProviders(
		Config{
			Environment: "production",

			WhatsAppDefaultProvider: "unknown",
		},
	)

	if err == nil {
		t.Fatal(
			"ValidateProductionProviders() accepted unsupported WhatsApp provider",
		)
	}
}

func TestValidateProductionProvidersRequiresCompleteMetaWebhookConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name        string
		verifyToken string
		appSecret   string
	}{
		{
			name: "verify token only",

			verifyToken: "verify-token",
		},
		{
			name: "app secret only",

			appSecret: "app-secret",
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				err := ValidateProductionProviders(
					Config{
						Environment: "production",

						OTPWebhookAddress: ":8081",

						MetaWebhookVerifyToken: testCase.verifyToken,

						MetaAppSecret: testCase.appSecret,
					},
				)

				if err == nil {
					t.Fatal(
						"ValidateProductionProviders() accepted incomplete Meta webhook configuration",
					)
				}
			},
		)
	}
}

func TestValidateProductionProvidersAcceptsCompleteMetaWebhookConfiguration(
	t *testing.T,
) {
	err := ValidateProductionProviders(
		Config{
			Environment: "production",

			OTPWebhookAddress: ":8081",

			MetaWebhookVerifyToken: "verify-token",

			MetaAppSecret: "app-secret",
		},
	)

	if err != nil {
		t.Fatalf(
			"ValidateProductionProviders() returned an error: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersRequiresWebhookAddressForTelnyxWebhook(
	t *testing.T,
) {
	err := ValidateProductionProviders(
		Config{
			Environment: "production",

			TelnyxPublicKey: "configured-public-key",
		},
	)

	if err == nil {
		t.Fatal(
			"ValidateProductionProviders() accepted Telnyx webhook without address",
		)
	}
}

func TestValidateProductionProvidersRequiresWebhookAddressForMetaWebhook(
	t *testing.T,
) {
	err := ValidateProductionProviders(
		Config{
			Environment: "production",

			MetaWebhookVerifyToken: "verify-token",

			MetaAppSecret: "app-secret",
		},
	)

	if err == nil {
		t.Fatal(
			"ValidateProductionProviders() accepted Meta webhook without address",
		)
	}
}

func TestValidateProductionProvidersAcceptsSMSDefaultAndFallback(
	t *testing.T,
) {
	cfg := Config{
		Environment: "production",

		SMSDefaultProvider: "bulksmsiraq",

		SMSFallbackProvider: "telnyx",

		BulkSMSIraqOTPEndpoint: "https://example.com/api/v5/otp/send",

		BulkSMSIraqAPIKey: "bulk-api-key",

		BulkSMSIraqSenderID: "sender",

		TelnyxEndpoint: "https://api.telnyx.com/v2/messages",

		TelnyxAPIKey: "telnyx-api-key",

		TelnyxFrom: "+15555550123",
	}

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() rejected valid SMS failover configuration: %v",
			err,
		)
	}
}

func TestValidateProductionProvidersAcceptsWhatsAppDefaultAndFallback(
	t *testing.T,
) {
	cfg := validMetaProductionConfig()

	cfg.WhatsAppDefaultProvider =
		"bulksmsiraq"

	cfg.WhatsAppFallbackProvider =
		"meta"

	cfg.BulkSMSIraqOTPEndpoint =
		"https://example.com/api/v5/otp/send"

	cfg.BulkSMSIraqAPIKey =
		"bulk-api-key"

	cfg.BulkSMSIraqSenderID =
		"sender"

	if err := ValidateProductionProviders(
		cfg,
	); err != nil {
		t.Fatalf(
			"ValidateProductionProviders() rejected valid WhatsApp failover configuration: %v",
			err,
		)
	}
}

func validMetaProductionConfig() Config {
	return Config{
		Environment: "production",

		WhatsAppDefaultProvider: "meta",

		MetaWhatsAppEndpoint: "https://graph.facebook.com/v23.0/phone/messages",

		MetaWhatsAppAccessToken: "test-access-token",

		MetaWhatsAppTemplateENName: "otp_en",

		MetaWhatsAppTemplateENLanguage: "en_US",

		MetaWhatsAppTemplateARName: "otp_ar",

		MetaWhatsAppTemplateARLanguage: "ar",

		MetaWhatsAppTemplateKUName: "otp_ku",

		MetaWhatsAppTemplateKULanguage: "ku",
	}
}
