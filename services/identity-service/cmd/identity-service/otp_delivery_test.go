package main

import (
	"net/http"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
)

func baseProductionOTPConfig() config.Config {
	return config.Config{
		OTPBrandName:           "Ride",
		OTPProviderHTTPTimeout: "10s",

		SMSDefaultProvider: "",
		SMSRoutes:          "+964=bulksmsiraq",

		TelnyxEndpoint: "https://api.telnyx.com/v2/messages",
		TelnyxAPIKey:   "test-telnyx-api-key",
		TelnyxFrom:     "Ride",

		TelnyxMessagingProfileID: "profile-123",

		WhatsAppDefaultProvider: "",
		WhatsAppRoutes:          "",

		MetaWhatsAppEndpoint: "https://graph.facebook.com/v23.0/123456789/messages",

		MetaWhatsAppAccessToken: "test-meta-access-token",

		MetaWhatsAppTemplateENName:     "ride_authentication",
		MetaWhatsAppTemplateENLanguage: "en_US",

		MetaWhatsAppTemplateARName:     "ride_authentication",
		MetaWhatsAppTemplateARLanguage: "ar",

		MetaWhatsAppTemplateKUName:     "",
		MetaWhatsAppTemplateKULanguage: "",

		BulkSMSIraqEndpoint: "https://sms.example.com/api/send",
		BulkSMSIraqAPIKey:   "test-sms-api-key",
		BulkSMSIraqSenderID: "Ride",

		EmailProvider: "resend",

		ResendEndpoint: "https://api.resend.com/emails",
		ResendAPIKey:   "test-email-api-key",
		ResendFrom:     "Ride <no-reply@example.com>",
	}
}

func TestBuildProductionOTPDeliveryRequiresGlobalSMSProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)

	if err == nil {
		t.Fatal(
			"buildProductionOTPDelivery() accepted missing global SMS provider",
		)
	}

	if delivery != nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned delivery without global SMS provider",
		)
	}
}

func TestBuildProductionOTPDeliveryRejectsUnsupportedGlobalSMSProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.SMSDefaultProvider = "unknown"

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)

	if err == nil {
		t.Fatal(
			"buildProductionOTPDelivery() accepted unsupported global SMS provider",
		)
	}

	if delivery != nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned delivery for unsupported global SMS provider",
		)
	}
}

func TestBuildProductionOTPDeliveryAcceptsTelnyxGlobalSMSProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.SMSDefaultProvider = "  TELNYX  "

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)
	if err != nil {
		t.Fatalf(
			"buildProductionOTPDelivery() returned an error: %v",
			err,
		)
	}

	if delivery == nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned nil delivery",
		)
	}
}

func TestBuildProductionOTPDeliveryRejectsInvalidTelnyxConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{
			name: "missing endpoint",
			mutate: func(cfg *config.Config) {
				cfg.TelnyxEndpoint = ""
			},
		},
		{
			name: "invalid endpoint",
			mutate: func(cfg *config.Config) {
				cfg.TelnyxEndpoint =
					"https://example.com/v2/messages"
			},
		},
		{
			name: "missing API key",
			mutate: func(cfg *config.Config) {
				cfg.TelnyxAPIKey = ""
			},
		},
		{
			name: "missing sender",
			mutate: func(cfg *config.Config) {
				cfg.TelnyxFrom = ""
			},
		},
		{
			name: "alphanumeric sender without messaging profile",
			mutate: func(cfg *config.Config) {
				cfg.TelnyxFrom = "Ride"
				cfg.TelnyxMessagingProfileID = ""
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := baseProductionOTPConfig()

			cfg.SMSDefaultProvider = "telnyx"

			testCase.mutate(
				&cfg,
			)

			delivery, err :=
				buildProductionOTPDelivery(
					cfg,
				)

			if err == nil {
				t.Fatal(
					"buildProductionOTPDelivery() accepted invalid Telnyx configuration",
				)
			}

			if delivery != nil {
				t.Fatal(
					"buildProductionOTPDelivery() returned delivery for invalid Telnyx configuration",
				)
			}
		})
	}
}

func TestBuildProductionOTPDeliveryRejectsInvalidHTTPTimeout(
	t *testing.T,
) {
	tests := []string{
		"",
		"not-a-duration",
		"0s",
		"-1s",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			cfg := baseProductionOTPConfig()

			cfg.OTPProviderHTTPTimeout = value

			delivery, err :=
				buildProductionOTPDelivery(
					cfg,
				)

			if err == nil {
				t.Fatal(
					"buildProductionOTPDelivery() accepted invalid HTTP timeout",
				)
			}

			if delivery != nil {
				t.Fatal(
					"buildProductionOTPDelivery() returned delivery for invalid HTTP timeout",
				)
			}
		})
	}
}

func TestBuildProductionOTPDeliveryAllowsWhatsAppToBeDisabled(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.SMSDefaultProvider = "bulksmsiraq"
	cfg.WhatsAppDefaultProvider = ""
	cfg.WhatsAppRoutes = ""

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)
	if err != nil {
		t.Fatalf(
			"buildProductionOTPDelivery() returned an error: %v",
			err,
		)
	}

	if delivery == nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned nil delivery",
		)
	}
}

func TestBuildProductionOTPDeliveryAcceptsMetaWhatsAppDefaultProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.SMSDefaultProvider = "bulksmsiraq"
	cfg.WhatsAppDefaultProvider = "  META  "

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)
	if err != nil {
		t.Fatalf(
			"buildProductionOTPDelivery() returned an error: %v",
			err,
		)
	}

	if delivery == nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned nil delivery",
		)
	}
}

func TestBuildProductionOTPDeliveryRejectsUnsupportedWhatsAppProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.SMSDefaultProvider = "bulksmsiraq"
	cfg.WhatsAppDefaultProvider = "unknown"

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)

	if err == nil {
		t.Fatal(
			"buildProductionOTPDelivery() accepted unsupported WhatsApp provider",
		)
	}

	if delivery != nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned delivery for unsupported WhatsApp provider",
		)
	}
}

func TestBuildProductionOTPDeliveryRejectsInvalidMetaWhatsAppConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{
			name: "missing endpoint",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppEndpoint = ""
			},
		},
		{
			name: "invalid endpoint",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppEndpoint =
					"https://example.com/v23.0/123456789/messages"
			},
		},
		{
			name: "missing access token",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppAccessToken = ""
			},
		},
		{
			name: "missing English template name",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppTemplateENName = ""
			},
		},
		{
			name: "missing English template language",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppTemplateENLanguage = ""
			},
		},
		{
			name: "Arabic template without language",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppTemplateARName =
					"ride_authentication"
				cfg.MetaWhatsAppTemplateARLanguage = ""
			},
		},
		{
			name: "Kurdish template without language",
			mutate: func(cfg *config.Config) {
				cfg.MetaWhatsAppTemplateKUName =
					"ride_authentication"
				cfg.MetaWhatsAppTemplateKULanguage = ""
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := baseProductionOTPConfig()

			cfg.SMSDefaultProvider = "bulksmsiraq"
			cfg.WhatsAppDefaultProvider = "meta"

			testCase.mutate(&cfg)

			delivery, err :=
				buildProductionOTPDelivery(
					cfg,
				)

			if err == nil {
				t.Fatal(
					"buildProductionOTPDelivery() accepted invalid Meta WhatsApp configuration",
				)
			}

			if delivery != nil {
				t.Fatal(
					"buildProductionOTPDelivery() returned delivery for invalid Meta WhatsApp configuration",
				)
			}
		})
	}
}

func TestBuildSMSRoutesParsesRegionalOverrides(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	routes, err := buildSMSRoutes(
		"+964=bulksmsiraq,+96475=bulksmsiraq",
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildSMSRoutes() returned an error: %v",
			err,
		)
	}

	if len(routes) != 2 {
		t.Fatalf(
			"route count = %d, expected 2",
			len(routes),
		)
	}

	if routes[0].PhonePrefix != "+964" {
		t.Fatalf(
			"first route prefix = %q, expected %q",
			routes[0].PhonePrefix,
			"+964",
		)
	}

	if routes[1].PhonePrefix != "+96475" {
		t.Fatalf(
			"second route prefix = %q, expected %q",
			routes[1].PhonePrefix,
			"+96475",
		)
	}
}

func TestBuildSMSRoutesAllowsNoRegionalOverrides(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	routes, err := buildSMSRoutes(
		"   ",
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildSMSRoutes() returned an error: %v",
			err,
		)
	}

	if len(routes) != 0 {
		t.Fatalf(
			"route count = %d, expected 0",
			len(routes),
		)
	}
}

func TestBuildSMSRoutesRejectsInvalidConfiguration(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	tests := []string{
		"964=bulksmsiraq",
		"+abc=bulksmsiraq",
		"+964",
		"+964=",
		"=bulksmsiraq",
		"+964=unknown",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			routes, err := buildSMSRoutes(
				value,
				&http.Client{},
				cfg,
			)

			if err == nil {
				t.Fatal(
					"buildSMSRoutes() accepted invalid configuration",
				)
			}

			if routes != nil {
				t.Fatal(
					"buildSMSRoutes() returned routes for invalid configuration",
				)
			}
		})
	}
}

func TestBuildWhatsAppSenderAllowsNoConfiguration(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.WhatsAppDefaultProvider = ""
	cfg.WhatsAppRoutes = ""

	sender, err := buildWhatsAppSender(
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildWhatsAppSender() returned an error: %v",
			err,
		)
	}

	if sender != nil {
		t.Fatal(
			"buildWhatsAppSender() returned sender without WhatsApp configuration",
		)
	}
}

func TestBuildWhatsAppSenderAcceptsMetaDefaultProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.WhatsAppDefaultProvider = "meta"

	sender, err := buildWhatsAppSender(
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildWhatsAppSender() returned an error: %v",
			err,
		)
	}

	if sender == nil {
		t.Fatal(
			"buildWhatsAppSender() returned nil sender",
		)
	}
}

func TestBuildWhatsAppSenderAllowsRegionalRoutesWithoutGlobalDefault(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.WhatsAppDefaultProvider = ""
	cfg.WhatsAppRoutes = "+964=meta"

	sender, err := buildWhatsAppSender(
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildWhatsAppSender() returned an error: %v",
			err,
		)
	}

	if sender == nil {
		t.Fatal(
			"buildWhatsAppSender() returned nil sender for regional route",
		)
	}
}

func TestBuildWhatsAppRoutesParsesRegionalOverrides(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	routes, err := buildWhatsAppRoutes(
		"+964=meta,+96475=meta",
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildWhatsAppRoutes() returned an error: %v",
			err,
		)
	}

	if len(routes) != 2 {
		t.Fatalf(
			"route count = %d, expected 2",
			len(routes),
		)
	}

	if routes[0].PhonePrefix != "+964" {
		t.Fatalf(
			"first route prefix = %q, expected %q",
			routes[0].PhonePrefix,
			"+964",
		)
	}

	if routes[1].PhonePrefix != "+96475" {
		t.Fatalf(
			"second route prefix = %q, expected %q",
			routes[1].PhonePrefix,
			"+96475",
		)
	}

	if routes[0].Provider == nil {
		t.Fatal(
			"first WhatsApp route has nil provider",
		)
	}

	if routes[1].Provider == nil {
		t.Fatal(
			"second WhatsApp route has nil provider",
		)
	}
}

func TestBuildWhatsAppRoutesAllowsNoRegionalOverrides(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	routes, err := buildWhatsAppRoutes(
		"   ",
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildWhatsAppRoutes() returned an error: %v",
			err,
		)
	}

	if len(routes) != 0 {
		t.Fatalf(
			"route count = %d, expected 0",
			len(routes),
		)
	}
}

func TestBuildWhatsAppRoutesRejectsInvalidConfiguration(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	tests := []string{
		"964=meta",
		"+abc=meta",
		"+964",
		"+964=",
		"=meta",
		"+964=unknown",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			routes, err := buildWhatsAppRoutes(
				value,
				&http.Client{},
				cfg,
			)

			if err == nil {
				t.Fatal(
					"buildWhatsAppRoutes() accepted invalid configuration",
				)
			}

			if routes != nil {
				t.Fatal(
					"buildWhatsAppRoutes() returned routes for invalid configuration",
				)
			}
		})
	}
}

func TestBuildWhatsAppProviderAcceptsMeta(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	provider, err := buildWhatsAppProvider(
		"  META  ",
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"buildWhatsAppProvider() returned nil provider",
		)
	}
}

func TestBuildWhatsAppProviderRejectsUnsupportedProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	provider, err := buildWhatsAppProvider(
		"unknown",
		&http.Client{},
		cfg,
	)

	if err == nil {
		t.Fatal(
			"buildWhatsAppProvider() accepted unsupported provider",
		)
	}

	if provider != nil {
		t.Fatal(
			"buildWhatsAppProvider() returned unsupported provider",
		)
	}
}

func TestBuildSMSProviderAcceptsTelnyx(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	provider, err := buildSMSProvider(
		"  TELNYX  ",
		&http.Client{},
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"buildSMSProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"buildSMSProvider() returned nil provider",
		)
	}
}
func TestNormalizeProviderName(
	t *testing.T,
) {
	actual := normalizeProviderName(
		"  BulkSMSIraq  ",
	)

	if actual != "bulksmsiraq" {
		t.Fatalf(
			"provider name = %q, expected %q",
			actual,
			"bulksmsiraq",
		)
	}
}

func TestBuildProductionOTPDeliveryRejectsUnsupportedEmailProvider(
	t *testing.T,
) {
	cfg := baseProductionOTPConfig()

	cfg.SMSDefaultProvider = "bulksmsiraq"
	cfg.EmailProvider = "unknown"

	delivery, err :=
		buildProductionOTPDelivery(
			cfg,
		)

	if err == nil {
		t.Fatal(
			"buildProductionOTPDelivery() accepted unsupported email provider",
		)
	}

	if delivery != nil {
		t.Fatal(
			"buildProductionOTPDelivery() returned delivery for unsupported email provider",
		)
	}
}
