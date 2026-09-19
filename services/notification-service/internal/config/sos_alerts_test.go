package config

import (
	"strings"
	"testing"
	"time"
)

func baseSOSConfig() Config {
	return Config{
		SOSAlertTimeout:  "10s",
		SOSRetryInterval: "15s",
		SOSGiveUpAfter:   "30m",
	}
}

func TestParseSOSAlertsNothingConfiguredIsValid(t *testing.T) {
	alerts, err := ParseSOSAlerts(baseSOSConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if alerts.Configured() || alerts.SMSEnabled() || alerts.WebhookEnabled() {
		t.Fatalf("nothing should be enabled: %+v", alerts)
	}

	if alerts.RetryInterval != 15*time.Second || alerts.GiveUpAfter != 30*time.Minute || alerts.Timeout != 10*time.Second {
		t.Fatalf("unexpected durations: %+v", alerts)
	}
}

func TestParseSOSAlertsSMS(t *testing.T) {
	cfg := baseSOSConfig()
	cfg.SOSOperatorPhones = " +9647701234567 , 9647809876543,9647701234567 ,"
	cfg.BulkSMSIraqEndpoint = "https://sms.example/send"
	cfg.BulkSMSIraqAPIKey = "key"
	cfg.BulkSMSIraqSenderID = "Ride"

	alerts, err := ParseSOSAlerts(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Join(alerts.OperatorPhones, ",") != "9647701234567,9647809876543" {
		t.Fatalf("phones must be trimmed, stripped of + and de-duplicated: %v", alerts.OperatorPhones)
	}

	if !alerts.SMSEnabled() || alerts.WebhookEnabled() || !alerts.Configured() {
		t.Fatalf("unexpected enabled channels: %+v", alerts)
	}
}

func TestParseSOSAlertsRejectsBadPhones(t *testing.T) {
	for _, phones := range []string{"07701234567a", "12345", "1234567890123456", "964 770 123 4567"} {
		cfg := baseSOSConfig()
		cfg.SOSOperatorPhones = phones
		cfg.BulkSMSIraqEndpoint = "https://sms.example/send"
		cfg.BulkSMSIraqAPIKey = "key"
		cfg.BulkSMSIraqSenderID = "Ride"

		if _, err := ParseSOSAlerts(cfg); err == nil {
			t.Fatalf("%q: expected an error", phones)
		}
	}
}

func TestParseSOSAlertsPhonesRequireTheWholeGatewayConfiguration(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"no endpoint": func(c *Config) { c.BulkSMSIraqEndpoint = "" },
		"no key":      func(c *Config) { c.BulkSMSIraqAPIKey = "" },
		"no sender":   func(c *Config) { c.BulkSMSIraqSenderID = "" },
	} {
		cfg := baseSOSConfig()
		cfg.SOSOperatorPhones = "9647701234567"
		cfg.BulkSMSIraqEndpoint = "https://sms.example/send"
		cfg.BulkSMSIraqAPIKey = "key"
		cfg.BulkSMSIraqSenderID = "Ride"

		mutate(&cfg)

		if _, err := ParseSOSAlerts(cfg); err == nil {
			t.Fatalf("%s: a half-configured SMS channel must fail startup", name)
		}
	}
}

func TestParseSOSAlertsRejectsBadURLs(t *testing.T) {
	cfg := baseSOSConfig()
	cfg.SOSWebhookURL = "not a url"

	if _, err := ParseSOSAlerts(cfg); err == nil {
		t.Fatalf("expected an error for a bad webhook URL")
	}

	cfg = baseSOSConfig()
	cfg.SOSOperatorPhones = "9647701234567"
	cfg.BulkSMSIraqEndpoint = "ftp://sms.example"
	cfg.BulkSMSIraqAPIKey = "key"
	cfg.BulkSMSIraqSenderID = "Ride"

	if _, err := ParseSOSAlerts(cfg); err == nil {
		t.Fatalf("expected an error for a non-http gateway endpoint")
	}
}

func TestParseSOSAlertsWebhookOnly(t *testing.T) {
	cfg := baseSOSConfig()
	cfg.SOSWebhookURL = "https://hooks.example/sos"
	cfg.SOSWebhookSecret = " s3cret "

	alerts, err := ParseSOSAlerts(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if alerts.SMSEnabled() || !alerts.WebhookEnabled() || alerts.WebhookSecret != "s3cret" {
		t.Fatalf("unexpected result: %+v", alerts)
	}
}

func TestParseSOSAlertsRejectsBadDurations(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"timeout": func(c *Config) { c.SOSAlertTimeout = "0s" },
		"retry":   func(c *Config) { c.SOSRetryInterval = "soon" },
		"give up": func(c *Config) { c.SOSGiveUpAfter = "-1m" },
		"empty":   func(c *Config) { c.SOSRetryInterval = "" },
	} {
		cfg := baseSOSConfig()
		mutate(&cfg)

		if _, err := ParseSOSAlerts(cfg); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestLoadDefaultsTheSOSSettings(t *testing.T) {
	for _, name := range []string{"SOS_OPERATOR_PHONES", "SOS_ALERT_TIMEOUT", "SOS_ALERT_RETRY_INTERVAL", "SOS_ALERT_GIVE_UP_AFTER"} {
		t.Setenv(name, "")
	}

	cfg := Load()

	if cfg.SOSOperatorPhones != "" || cfg.SOSAlertTimeout != "10s" || cfg.SOSRetryInterval != "15s" || cfg.SOSGiveUpAfter != "30m" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
