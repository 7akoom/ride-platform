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

// buildStagingOTPDelivery is the development delivery, and with
// OTP_STAGING_WHATSAPP=true it also sends phone codes by the instance's
// WhatsApp provider (BulkSMSIraq), so staging can be tried as a customer would.
func buildStagingOTPDelivery(
	environment string,
	cfg config.Config,
	development auth.OTPDelivery,
	trackingStore otp.DeliveryTrackingStore,
	metricsRecorder otp.DeliveryMetricsRecorder,
	logger *slog.Logger,
) (auth.OTPDelivery, error) {
	if !cfg.OTPStagingWhatsApp {
		return development, nil
	}

	httpTimeout, err := time.ParseDuration(strings.TrimSpace(cfg.OTPProviderHTTPTimeout))
	if err != nil || httpTimeout <= 0 {
		return nil, errors.New("OTP provider HTTP timeout must be a positive duration")
	}

	var metrics []otp.DeliveryMetricsRecorder
	if metricsRecorder != nil {
		metrics = append(metrics, metricsRecorder)
	}

	whatsApp, err := buildWhatsAppSenderWithTracking(
		&http.Client{Timeout: httpTimeout},
		cfg,
		trackingStore,
		metrics...,
	)
	if err != nil {
		return nil, fmt.Errorf("configure staging WhatsApp codes: %w", err)
	}

	if whatsApp == nil {
		return nil, errors.New("OTP_STAGING_WHATSAPP=true needs WHATSAPP_DEFAULT_PROVIDER (bulksmsiraq)")
	}

	delivery, err := otp.NewStagingDelivery(environment, development, whatsApp, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("staging: phone codes are sent by WhatsApp as well as written to the log",
		"provider", cfg.WhatsAppDefaultProvider,
	)

	return delivery, nil
}
