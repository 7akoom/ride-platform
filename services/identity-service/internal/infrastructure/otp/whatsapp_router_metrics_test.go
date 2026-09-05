package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestWhatsAppRouterRecordsSuccessfulRouteDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	provider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: DeliveryMetricProviderMeta,
				Provider:     provider,
			},
		},
		nil,
		WithWhatsAppRouterDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	err = router.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "ar",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	requireSingleDeliveryMetric(
		t,
		metricsRecorder,
		DeliveryMetricChannelWhatsApp,
		DeliveryMetricProviderMeta,
		DeliveryMetricOutcomeSuccess,
	)
}

func TestWhatsAppRouterRecordsSuccessfulDefaultDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	provider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		nil,
		provider,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProviderMeta,
		),
		WithWhatsAppRouterDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	err = router.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+971501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "en",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	requireSingleDeliveryMetric(
		t,
		metricsRecorder,
		DeliveryMetricChannelWhatsApp,
		DeliveryMetricProviderMeta,
		DeliveryMetricOutcomeSuccess,
	)
}

func TestWhatsAppRouterRecordsFailedRouteDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	providerErr := errors.New(
		"WhatsApp provider unavailable",
	)

	provider := &testWhatsAppProvider{
		err: providerErr,
	}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: DeliveryMetricProviderMeta,
				Provider:     provider,
			},
		},
		nil,
		WithWhatsAppRouterDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	err = router.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "ar",
		},
	)
	if !errors.Is(
		err,
		providerErr,
	) {
		t.Fatalf(
			"SendOTP() error = %v, expected wrapped %v",
			err,
			providerErr,
		)
	}

	requireSingleDeliveryMetric(
		t,
		metricsRecorder,
		DeliveryMetricChannelWhatsApp,
		DeliveryMetricProviderMeta,
		DeliveryMetricOutcomeFailed,
	)
}

func TestWhatsAppRouterDoesNotRecordMetricWithoutProviderAttempt(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: DeliveryMetricProviderMeta,
				Provider:     &testWhatsAppProvider{},
			},
		},
		nil,
		WithWhatsAppRouterDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	err = router.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+971501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "en",
		},
	)
	if err == nil {
		t.Fatal(
			"SendOTP() accepted destination without provider",
		)
	}

	if len(metricsRecorder.deliveries) != 0 {
		t.Fatalf(
			"delivery metric count = %d, expected 0",
			len(metricsRecorder.deliveries),
		)
	}
}

func TestWhatsAppRouterRequiresProviderNamesWhenMetricsAreEnabled(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}

	tests := []struct {
		name            string
		routes          []WhatsAppRoute
		defaultProvider WhatsAppProvider
		options         []WhatsAppRouterOption
	}{
		{
			name: "route without provider name",
			routes: []WhatsAppRoute{
				{
					PhonePrefix: "+964",
					Provider:    &testWhatsAppProvider{},
				},
			},
			options: []WhatsAppRouterOption{
				WithWhatsAppRouterDeliveryMetricsRecorder(
					metricsRecorder,
				),
			},
		},
		{
			name:            "default provider without provider name",
			defaultProvider: &testWhatsAppProvider{},
			options: []WhatsAppRouterOption{
				WithWhatsAppRouterDeliveryMetricsRecorder(
					metricsRecorder,
				),
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			router, err := NewWhatsAppRouter(
				testCase.routes,
				testCase.defaultProvider,
				testCase.options...,
			)

			if err == nil {
				t.Fatal(
					"NewWhatsAppRouter() accepted missing provider metric metadata",
				)
			}

			if router != nil {
				t.Fatal(
					"NewWhatsAppRouter() returned router with invalid metric metadata",
				)
			}
		})
	}
}
