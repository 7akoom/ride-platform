package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

// buildStagingOTPDelivery is the development delivery. With
// OTP_STAGING_WHATSAPP=true it also sends phone codes by the instance's
// WhatsApp provider (BulkSMSIraq), and with OTP_STAGING_EMAIL=true email codes
// by its email provider (Resend), so staging can be tried as a customer would.
func buildStagingOTPDelivery(
	environment string,
	cfg config.Config,
	development auth.OTPDelivery,
	trackingStore otp.DeliveryTrackingStore,
	metricsRecorder otp.DeliveryMetricsRecorder,
	logger *slog.Logger,
) (auth.OTPDelivery, error) {
	if !cfg.OTPStagingWhatsApp && !cfg.OTPStagingEmail {
		return development, nil
	}

	httpTimeout, err := time.ParseDuration(strings.TrimSpace(cfg.OTPProviderHTTPTimeout))
	if err != nil || httpTimeout <= 0 {
		return nil, errors.New("OTP provider HTTP timeout must be a positive duration")
	}

	httpClient := &http.Client{Timeout: httpTimeout}

	var metrics []otp.DeliveryMetricsRecorder
	if metricsRecorder != nil {
		metrics = append(metrics, metricsRecorder)
	}

	var whatsApp otp.WhatsAppSender

	if cfg.OTPStagingWhatsApp {
		whatsApp, err = buildWhatsAppSenderWithTracking(httpClient, cfg, trackingStore, metrics...)
		if err != nil {
			return nil, fmt.Errorf("configure staging WhatsApp codes: %w", err)
		}

		if whatsApp == nil {
			return nil, errors.New("OTP_STAGING_WHATSAPP=true needs WHATSAPP_DEFAULT_PROVIDER (bulksmsiraq)")
		}
	}

	var email otp.EmailSender

	if cfg.OTPStagingEmail {
		sender, err := buildEmailSender(httpClient, cfg, metricsRecorder)
		if err != nil {
			return nil, fmt.Errorf("configure staging email codes (RESEND_API_KEY, RESEND_FROM): %w", err)
		}

		email = sender
	}

	delivery, err := otp.NewStagingDelivery(environment, development, whatsApp, email, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("staging: codes are sent for real; only those that cannot be go to the log",
		"whatsapp", cfg.OTPStagingWhatsApp,
		"email", cfg.OTPStagingEmail,
	)

	return delivery, nil
}
