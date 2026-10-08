package main

import (
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestBuildProductionOTPDeliveryWhatsAppOnlyPhoneCodes(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()
	cfg.SMSRoutes = ""
	cfg.OTPPhoneDefaultChannel = "whatsapp"
	cfg.WhatsAppDefaultProvider = "bulksmsiraq"
	cfg.BulkSMSIraqOTPEndpoint = "https://sms.example.com/api/v5/otp/send"

	delivery, err := buildProductionOTPDelivery(cfg)
	if err != nil {
		t.Fatalf("WhatsApp-only phone codes: %v", err)
	}

	if delivery == nil {
		t.Fatal("no delivery")
	}
}

func TestBuildProductionOTPDeliveryWhatsAppDefaultNeedsAProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()
	cfg.SMSRoutes = ""
	cfg.OTPPhoneDefaultChannel = "whatsapp"

	if delivery, err := buildProductionOTPDelivery(cfg); err == nil || delivery != nil {
		t.Fatalf("accepted WhatsApp by default with no WhatsApp provider: %v", err)
	}
}

func TestBuildProductionOTPDeliverySMSDefaultStillNeedsSMS(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()
	cfg.SMSRoutes = ""
	cfg.WhatsAppDefaultProvider = "bulksmsiraq"
	cfg.BulkSMSIraqOTPEndpoint = "https://sms.example.com/api/v5/otp/send"

	if delivery, err := buildProductionOTPDelivery(cfg); err == nil || delivery != nil {
		t.Fatalf("accepted SMS by default with no SMS provider: %v", err)
	}
}

func TestPhoneAutoChannelFromConfig(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	for value, want := range map[string]auth.OTPDeliveryChannel{
		"":           auth.OTPDeliveryChannelSMS,
		"sms":        auth.OTPDeliveryChannelSMS,
		" WhatsApp ": auth.OTPDeliveryChannelWhatsApp,
	} {
		cfg.OTPPhoneDefaultChannel = value

		got, err := phoneAutoChannelFromConfig(cfg)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", value, got, err, want)
		}
	}

	cfg.OTPPhoneDefaultChannel = "telegram"
	if _, err := phoneAutoChannelFromConfig(cfg); err == nil {
		t.Error("accepted telegram")
	}
}
