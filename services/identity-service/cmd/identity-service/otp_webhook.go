package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	httptransport "github.com/7akoom/ride-platform/services/identity-service/internal/transport/http"
)

func buildOTPWebhookServer(
	cfg config.Config,
	receiptStore otp.DeliveryReceiptStore,
	metricsRecorders ...httptransport.DeliveryWebhookMetricsRecorder,
) (*httptransport.Server, error) {
	if len(metricsRecorders) > 1 || (len(metricsRecorders) == 1 && metricsRecorders[0] == nil) {
		return nil, errors.New("exactly one non-nil webhook metrics recorder is supported when supplied")
	}
	metricOptions := func(provider otp.DeliveryTrackingProvider) []httptransport.DeliveryReceiptHandlerOption {
		if len(metricsRecorders) == 0 {
			return nil
		}
		return []httptransport.DeliveryReceiptHandlerOption{
			httptransport.WithDeliveryWebhookMetrics(provider, metricsRecorders[0]),
		}
	}

	routes := make(
		[]httptransport.WebhookRoute,
		0,
		2,
	)

	telnyxPublicKey := strings.TrimSpace(
		cfg.TelnyxPublicKey,
	)

	if telnyxPublicKey != "" {
		if receiptStore == nil {
			return nil, errors.New(
				"OTP delivery receipt store is required when Telnyx webhooks are enabled",
			)
		}

		decoder, err :=
			otp.NewTelnyxDeliveryReceiptDecoder(
				telnyxPublicKey,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure Telnyx delivery receipt decoder: %w",
				err,
			)
		}

		handler, err :=
			httptransport.NewDeliveryReceiptHandler(
				decoder,
				receiptStore,
				metricOptions(otp.DeliveryTrackingProviderTelnyx)...,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure Telnyx delivery receipt handler: %w",
				err,
			)
		}

		routes = append(
			routes,
			httptransport.WebhookRoute{
				Provider: "telnyx",
				Handler:  handler,
			},
		)
	}

	metaVerifyToken := strings.TrimSpace(
		cfg.MetaWebhookVerifyToken,
	)

	metaAppSecret := strings.TrimSpace(
		cfg.MetaAppSecret,
	)

	metaWebhookConfigured :=
		metaVerifyToken != "" ||
			metaAppSecret != ""

	if metaWebhookConfigured {
		if metaVerifyToken == "" {
			return nil, errors.New(
				"Meta webhook verify token is required when Meta webhooks are enabled",
			)
		}

		if metaAppSecret == "" {
			return nil, errors.New(
				"Meta app secret is required when Meta webhooks are enabled",
			)
		}

		if receiptStore == nil {
			return nil, errors.New(
				"OTP delivery receipt store is required when Meta webhooks are enabled",
			)
		}

		verificationHandler, err :=
			otp.NewMetaWebhookVerificationHandler(
				metaVerifyToken,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure Meta webhook verification handler: %w",
				err,
			)
		}

		decoder, err :=
			otp.NewMetaDeliveryReceiptDecoder(
				metaAppSecret,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure Meta delivery receipt decoder: %w",
				err,
			)
		}

		handler, err :=
			httptransport.NewDeliveryReceiptBatchHandler(
				decoder,
				receiptStore,
				metricOptions(otp.DeliveryTrackingProviderMeta)...,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure Meta delivery receipt handler: %w",
				err,
			)
		}

		routes = append(
			routes,
			httptransport.WebhookRoute{
				Provider:   "meta",
				Handler:    handler,
				GetHandler: verificationHandler,
			},
		)
	}

	if len(routes) == 0 {
		return nil, nil
	}

	router, err := httptransport.NewWebhookRouter(
		routes,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure OTP webhook router: %w",
			err,
		)
	}

	server, err := httptransport.NewServer(
		cfg.OTPWebhookAddress,
		router,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure OTP webhook server: %w",
			err,
		)
	}

	return server, nil
}
