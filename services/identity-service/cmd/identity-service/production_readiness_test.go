package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
)

type noSendTransport struct{ calls int }

func (r *noSendTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return nil, errors.New("network requests forbidden during construction validation")
}

func productionReadinessConfig() config.Config {
	cfg := baseProductionOTPConfig()
	cfg.Environment = " Production "
	cfg.SMSDefaultProvider = "bulksmsiraq"
	cfg.SMSFallbackProvider = "telnyx"
	cfg.WhatsAppDefaultProvider = "bulksmsiraq"
	cfg.WhatsAppFallbackProvider = "meta"
	cfg.BulkSMSIraqOTPEndpoint = "https://sms.example.com/api/otp/send"
	cfg.MetaWhatsAppTemplateKUName = "ride_authentication"
	cfg.MetaWhatsAppTemplateKULanguage = "ckb"
	return cfg
}

func TestProductionProviderConstructionDoesNotSend(t *testing.T) {
	transport := &noSendTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, whatsappEnabled := range []bool{true, false} {
		cfg := productionReadinessConfig()
		if !whatsappEnabled {
			cfg.WhatsAppDefaultProvider, cfg.WhatsAppFallbackProvider = "", ""
		}
		if err := config.ValidateProductionProviders(cfg); err != nil {
			t.Fatal(err)
		}
		delivery, err := buildProductionOTPDelivery(cfg)
		if err != nil || delivery == nil {
			t.Fatal("valid production configuration failed construction")
		}
	}
	if transport.calls != 0 {
		t.Fatal("provider construction attempted a network request")
	}
}

func TestProductionProviderConstructionRejectsIncompleteConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"sms_required", func(c *config.Config) { c.SMSDefaultProvider = "" }},
		{"sms_fallback_matches_primary", func(c *config.Config) { c.SMSFallbackProvider = c.SMSDefaultProvider }},
		{"whatsapp_fallback_matches_primary", func(c *config.Config) { c.WhatsAppFallbackProvider = c.WhatsAppDefaultProvider }},
		{"telnyx_key", func(c *config.Config) { c.TelnyxAPIKey = "" }},
		{"telnyx_sender", func(c *config.Config) { c.TelnyxFrom, c.TelnyxMessagingProfileID = "", "" }},
		{"meta_access_token", func(c *config.Config) { c.MetaWhatsAppAccessToken = "" }},
		{"meta_template", func(c *config.Config) { c.MetaWhatsAppTemplateKUName = "" }},
		{"bulksmsiraq_key", func(c *config.Config) { c.BulkSMSIraqAPIKey = "" }},
		{"bulksmsiraq_otp_endpoint", func(c *config.Config) { c.BulkSMSIraqOTPEndpoint = "" }},
		{"bulksmsiraq_endpoint", func(c *config.Config) { c.BulkSMSIraqEndpoint = "" }},
		{"resend_key", func(c *config.Config) { c.ResendAPIKey = "" }},
		{"resend_from", func(c *config.Config) { c.ResendFrom = "" }},
		{"email_required", func(c *config.Config) { c.EmailProvider = "" }},
		{"http_timeout", func(c *config.Config) { c.OTPProviderHTTPTimeout = "0s" }},
		{"sms_threshold", func(c *config.Config) { c.SMSProviderHealthFailureThreshold = "0" }},
		{"whatsapp_cooldown", func(c *config.Config) { c.WhatsAppProviderHealthCooldown = "0s" }},
		{"webhook_pair", func(c *config.Config) { c.MetaWebhookVerifyToken = "test-only" }},
		{"webhook_address", func(c *config.Config) {
			c.MetaWebhookVerifyToken, c.MetaAppSecret = "test-only", "test-only"
			c.OTPWebhookAddress = ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := productionReadinessConfig()
			tc.mutate(&cfg)
			if config.ValidateProductionProviders(cfg) != nil {
				return
			}
			delivery, err := buildProductionOTPDelivery(cfg)
			if err == nil || delivery != nil {
				t.Fatal("incomplete production configuration was accepted")
			}
		})
	}
}
