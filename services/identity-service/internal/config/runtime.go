package config

import (
	"fmt"
	"strings"
	"time"
)

func ValidateRuntime(
	natsConfig NATS,
	outboxConfig Outbox,
) error {
	if natsConfig.PublishTimeout <= 0 {
		return fmt.Errorf(
			"NATS publish timeout must be positive",
		)
	}

	if outboxConfig.LeaseDuration <= 0 {
		return fmt.Errorf(
			"outbox lease duration must be positive",
		)
	}

	if outboxConfig.BatchSize <= 0 {
		return fmt.Errorf(
			"outbox batch size must be positive",
		)
	}

	maxPublishTimeout :=
		maxPublishTimeoutForLease(
			outboxConfig.LeaseDuration,
			outboxConfig.BatchSize,
		)

	if natsConfig.PublishTimeout >
		maxPublishTimeout {
		return fmt.Errorf(
			"OUTBOX_BATCH_SIZE multiplied by NATS_PUBLISH_TIMEOUT must be less than OUTBOX_LEASE_DURATION",
		)
	}

	return nil
}

func ValidateProductionProviders(
	cfg Config,
) error {
	environment := strings.ToLower(
		strings.TrimSpace(
			cfg.Environment,
		),
	)

	if environment != "production" {
		return nil
	}

	smsDefaultProvider := normalizeProviderName(
		cfg.SMSDefaultProvider,
	)

	smsFallbackProvider := normalizeProviderName(
		cfg.SMSFallbackProvider,
	)

	if smsDefaultProvider != "" {
		if err := validateSMSProvider(
			cfg,
			smsDefaultProvider,
		); err != nil {
			return err
		}
	}

	if smsFallbackProvider != "" {
		if err := validateSMSProvider(
			cfg,
			smsFallbackProvider,
		); err != nil {
			return err
		}
	}

	if smsDefaultProvider != "" &&
		smsFallbackProvider != "" &&
		smsDefaultProvider == smsFallbackProvider {
		return fmt.Errorf(
			"SMS_DEFAULT_PROVIDER and SMS_FALLBACK_PROVIDER must be different",
		)
	}

	whatsAppDefaultProvider := normalizeProviderName(
		cfg.WhatsAppDefaultProvider,
	)

	whatsAppFallbackProvider := normalizeProviderName(
		cfg.WhatsAppFallbackProvider,
	)

	if whatsAppDefaultProvider != "" {
		if err := validateWhatsAppProvider(
			cfg,
			whatsAppDefaultProvider,
		); err != nil {
			return err
		}
	}

	if whatsAppFallbackProvider != "" {
		if err := validateWhatsAppProvider(
			cfg,
			whatsAppFallbackProvider,
		); err != nil {
			return err
		}
	}

	if whatsAppDefaultProvider != "" &&
		whatsAppFallbackProvider != "" &&
		whatsAppDefaultProvider ==
			whatsAppFallbackProvider {
		return fmt.Errorf(
			"WHATSAPP_DEFAULT_PROVIDER and WHATSAPP_FALLBACK_PROVIDER must be different",
		)
	}

	if err := validateWebhookConfiguration(
		cfg,
	); err != nil {
		return err
	}

	return nil
}

func validateSMSProvider(
	cfg Config,
	provider string,
) error {
	switch provider {
	case "bulksmsiraq":
		if err := requireRuntimeValue(
			"BULKSMSIRAQ_OTP_ENDPOINT",
			cfg.BulkSMSIraqOTPEndpoint,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"BULKSMSIRAQ_API_KEY",
			cfg.BulkSMSIraqAPIKey,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"BULKSMSIRAQ_SENDER_ID",
			cfg.BulkSMSIraqSenderID,
		); err != nil {
			return err
		}

	case "telnyx":
		if err := requireRuntimeValue(
			"TELNYX_ENDPOINT",
			cfg.TelnyxEndpoint,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"TELNYX_API_KEY",
			cfg.TelnyxAPIKey,
		); err != nil {
			return err
		}

		if strings.TrimSpace(
			cfg.TelnyxFrom,
		) == "" &&
			strings.TrimSpace(
				cfg.TelnyxMessagingProfileID,
			) == "" {
			return fmt.Errorf(
				"TELNYX_FROM or TELNYX_MESSAGING_PROFILE_ID is required",
			)
		}

	default:
		return fmt.Errorf(
			"unsupported SMS provider %q",
			provider,
		)
	}

	return nil
}

func validateWhatsAppProvider(
	cfg Config,
	provider string,
) error {
	switch provider {
	case "bulksmsiraq":
		if err := requireRuntimeValue(
			"BULKSMSIRAQ_OTP_ENDPOINT",
			cfg.BulkSMSIraqOTPEndpoint,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"BULKSMSIRAQ_API_KEY",
			cfg.BulkSMSIraqAPIKey,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"BULKSMSIRAQ_SENDER_ID",
			cfg.BulkSMSIraqSenderID,
		); err != nil {
			return err
		}

	case "meta":
		if err := requireRuntimeValue(
			"META_WHATSAPP_ENDPOINT",
			cfg.MetaWhatsAppEndpoint,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_ACCESS_TOKEN",
			cfg.MetaWhatsAppAccessToken,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_TEMPLATE_EN_NAME",
			cfg.MetaWhatsAppTemplateENName,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_TEMPLATE_EN_LANGUAGE",
			cfg.MetaWhatsAppTemplateENLanguage,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_TEMPLATE_AR_NAME",
			cfg.MetaWhatsAppTemplateARName,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_TEMPLATE_AR_LANGUAGE",
			cfg.MetaWhatsAppTemplateARLanguage,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_TEMPLATE_KU_NAME",
			cfg.MetaWhatsAppTemplateKUName,
		); err != nil {
			return err
		}

		if err := requireRuntimeValue(
			"META_WHATSAPP_TEMPLATE_KU_LANGUAGE",
			cfg.MetaWhatsAppTemplateKULanguage,
		); err != nil {
			return err
		}

	default:
		return fmt.Errorf(
			"unsupported WhatsApp provider %q",
			provider,
		)
	}

	return nil
}

func validateWebhookConfiguration(
	cfg Config,
) error {
	telnyxPublicKey := strings.TrimSpace(
		cfg.TelnyxPublicKey,
	)

	metaVerifyToken := strings.TrimSpace(
		cfg.MetaWebhookVerifyToken,
	)

	metaAppSecret := strings.TrimSpace(
		cfg.MetaAppSecret,
	)

	webhookConfigured :=
		telnyxPublicKey != "" ||
			metaVerifyToken != "" ||
			metaAppSecret != ""

	if webhookConfigured {
		if err := requireRuntimeValue(
			"OTP_WEBHOOK_ADDRESS",
			cfg.OTPWebhookAddress,
		); err != nil {
			return err
		}
	}

	if (metaVerifyToken == "") !=
		(metaAppSecret == "") {
		return fmt.Errorf(
			"META_WEBHOOK_VERIFY_TOKEN and META_APP_SECRET must be configured together",
		)
	}

	return nil
}

func requireRuntimeValue(
	name string,
	value string,
) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf(
			"%s is required",
			name,
		)
	}

	return nil
}

func normalizeProviderName(
	provider string,
) string {
	return strings.ToLower(
		strings.TrimSpace(
			provider,
		),
	)
}

func maxPublishTimeoutForLease(
	leaseDuration time.Duration,
	batchSize int,
) time.Duration {
	if leaseDuration <= 0 ||
		batchSize <= 0 {
		return 0
	}

	return (leaseDuration - 1) /
		time.Duration(batchSize)
}
