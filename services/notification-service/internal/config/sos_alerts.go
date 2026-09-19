package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// SOSAlerts is the validated configuration for telling a human at the
// operating company that somebody pressed SOS.
type SOSAlerts struct {
	// OperatorPhones get an SMS through the BulkSMSIraq gateway. Digits
	// only, no leading +, exactly as the gateway expects them.
	OperatorPhones []string
	SMSEndpoint    string
	SMSAPIKey      string
	SMSSenderID    string

	// WebhookURL, when set, gets a JSON POST for every alert. If
	// WebhookSecret is also set the body is signed (HMAC-SHA256, hex, in
	// the X-Signature-256 header as "sha256=<hex>").
	WebhookURL    string
	WebhookSecret string

	Timeout       time.Duration
	RetryInterval time.Duration
	GiveUpAfter   time.Duration
}

func (a SOSAlerts) SMSEnabled() bool     { return len(a.OperatorPhones) > 0 }
func (a SOSAlerts) WebhookEnabled() bool { return a.WebhookURL != "" }

// Configured reports whether at least one channel will carry an alert.
func (a SOSAlerts) Configured() bool { return a.SMSEnabled() || a.WebhookEnabled() }

// ParseSOSAlerts validates the settings. Nothing configured is valid (the
// service runs, and says loudly on every SOS that nobody was alerted); a
// half-configured channel is a mistake and fails startup, because an alert
// channel that silently does nothing is worse than none.
func ParseSOSAlerts(cfg Config) (SOSAlerts, error) {
	timeout, err := parsePositiveDuration("SOS_ALERT_TIMEOUT", cfg.SOSAlertTimeout)
	if err != nil {
		return SOSAlerts{}, err
	}

	retryInterval, err := parsePositiveDuration("SOS_ALERT_RETRY_INTERVAL", cfg.SOSRetryInterval)
	if err != nil {
		return SOSAlerts{}, err
	}

	giveUpAfter, err := parsePositiveDuration("SOS_ALERT_GIVE_UP_AFTER", cfg.SOSGiveUpAfter)
	if err != nil {
		return SOSAlerts{}, err
	}

	alerts := SOSAlerts{
		Timeout:       timeout,
		RetryInterval: retryInterval,
		GiveUpAfter:   giveUpAfter,
	}

	phones, err := parseOperatorPhones(cfg.SOSOperatorPhones)
	if err != nil {
		return SOSAlerts{}, err
	}

	if len(phones) > 0 {
		endpoint := strings.TrimSpace(cfg.BulkSMSIraqEndpoint)
		apiKey := strings.TrimSpace(cfg.BulkSMSIraqAPIKey)
		senderID := strings.TrimSpace(cfg.BulkSMSIraqSenderID)

		if endpoint == "" || apiKey == "" || senderID == "" {
			return SOSAlerts{}, fmt.Errorf(
				"SOS_OPERATOR_PHONES is set, so BULKSMSIRAQ_ENDPOINT, BULKSMSIRAQ_API_KEY and BULKSMSIRAQ_SENDER_ID are all required",
			)
		}

		if err := validateHTTPURL("BULKSMSIRAQ_ENDPOINT", endpoint); err != nil {
			return SOSAlerts{}, err
		}

		alerts.OperatorPhones = phones
		alerts.SMSEndpoint = endpoint
		alerts.SMSAPIKey = apiKey
		alerts.SMSSenderID = senderID
	}

	webhookURL := strings.TrimSpace(cfg.SOSWebhookURL)
	if webhookURL != "" {
		if err := validateHTTPURL("SOS_WEBHOOK_URL", webhookURL); err != nil {
			return SOSAlerts{}, err
		}

		alerts.WebhookURL = webhookURL
		alerts.WebhookSecret = strings.TrimSpace(cfg.SOSWebhookSecret)
	}

	return alerts, nil
}

// parseOperatorPhones reads a comma-separated list, drops a leading + from
// each number and removes duplicates. Every number must be digits only.
func parseOperatorPhones(raw string) ([]string, error) {
	var phones []string

	seen := map[string]struct{}{}

	for _, part := range strings.Split(raw, ",") {
		phone := strings.TrimPrefix(strings.TrimSpace(part), "+")
		if phone == "" {
			continue
		}

		if len(phone) < 8 || len(phone) > 15 {
			return nil, fmt.Errorf("SOS_OPERATOR_PHONES entry %q must be 8 to 15 digits", part)
		}

		for _, character := range phone {
			if character < '0' || character > '9' {
				return nil, fmt.Errorf("SOS_OPERATOR_PHONES entry %q must contain only digits", part)
			}
		}

		if _, duplicate := seen[phone]; duplicate {
			continue
		}

		seen[phone] = struct{}{}
		phones = append(phones, phone)
	}

	return phones, nil
}

func validateHTTPURL(name string, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an http(s) URL", name)
	}

	return nil
}
