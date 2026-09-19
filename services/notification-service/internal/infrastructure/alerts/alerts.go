package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/notification-service/internal/config"
)

type channel struct {
	name    string
	alerter events.OperatorAlerter
}

// Multi tries every configured channel. The alert counts as delivered once
// ANY channel got it through: a dead webhook must not turn a delivered SMS
// into a retry that pages the operators again.
type Multi struct {
	channels []channel
	logger   *slog.Logger
}

func (m *Multi) Alert(ctx context.Context, alert events.SOSAlert) error {
	var failures []error

	delivered := 0

	for _, ch := range m.channels {
		if err := ch.alerter.Alert(ctx, alert); err != nil {
			m.logger.WarnContext(ctx, "SOS operator alert channel failed",
				"channel", ch.name,
				"alert_id", alert.AlertID,
				"error", err,
			)

			failures = append(failures, fmt.Errorf("%s: %w", ch.name, err))

			continue
		}

		m.logger.InfoContext(ctx, "SOS operator alert delivered",
			"channel", ch.name,
			"alert_id", alert.AlertID,
			"trip_id", alert.TripID,
		)

		delivered++
	}

	if delivered > 0 {
		return nil
	}

	return errors.Join(failures...)
}

// New builds the alerter for a deployment. It returns nil (and no error)
// when no channel is configured; the caller then runs without operator
// alerts, and the handler says loudly on every SOS that nobody was alerted.
func New(cfg config.SOSAlerts, logger *slog.Logger) (events.OperatorAlerter, error) {
	if !cfg.Configured() {
		return nil, nil
	}

	client := &http.Client{Timeout: cfg.Timeout}
	multi := &Multi{logger: logger}

	if cfg.SMSEnabled() {
		sms, err := NewSMS(client, cfg.SMSEndpoint, cfg.SMSAPIKey, cfg.SMSSenderID, cfg.OperatorPhones, logger)
		if err != nil {
			return nil, fmt.Errorf("build SMS alerter: %w", err)
		}

		multi.channels = append(multi.channels, channel{name: "sms", alerter: sms})
	}

	if cfg.WebhookEnabled() {
		webhook, err := NewWebhook(client, cfg.WebhookURL, cfg.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("build webhook alerter: %w", err)
		}

		multi.channels = append(multi.channels, channel{name: "webhook", alerter: webhook})
	}

	return multi, nil
}
