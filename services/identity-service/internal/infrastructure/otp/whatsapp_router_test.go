package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type testWhatsAppProvider struct {
	calls int
	err   error

	input WhatsAppOTPProviderInput
}

func (p *testWhatsAppProvider) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	p.calls++
	p.input = input

	return p.err
}

func TestWhatsAppRouterRoutesByPhonePrefix(
	t *testing.T,
) {
	iraqProvider := &testWhatsAppProvider{}
	defaultProvider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		defaultProvider,
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	input := WhatsAppOTPProviderInput{
		PhoneNumber: "+9647501234567",
		Code:        "123456",
		Purpose:     auth.OTPPurposeLogin,
		Locale:      "ar",
	}

	err = router.SendOTP(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	if iraqProvider.calls != 1 {
		t.Fatalf(
			"Iraq provider calls = %d, expected 1",
			iraqProvider.calls,
		)
	}

	if defaultProvider.calls != 0 {
		t.Fatalf(
			"default provider calls = %d, expected 0",
			defaultProvider.calls,
		)
	}

	if iraqProvider.input != input {
		t.Fatalf(
			"Iraq provider input = %+v, expected %+v",
			iraqProvider.input,
			input,
		)
	}
}

func TestWhatsAppRouterUsesDefaultProvider(
	t *testing.T,
) {
	iraqProvider := &testWhatsAppProvider{}
	defaultProvider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		defaultProvider,
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	input := WhatsAppOTPProviderInput{
		PhoneNumber: "+971501234567",
		Code:        "123456",
		Purpose:     auth.OTPPurposeLogin,
		Locale:      "en",
	}

	err = router.SendOTP(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	if defaultProvider.calls != 1 {
		t.Fatalf(
			"default provider calls = %d, expected 1",
			defaultProvider.calls,
		)
	}

	if iraqProvider.calls != 0 {
		t.Fatalf(
			"Iraq provider calls = %d, expected 0",
			iraqProvider.calls,
		)
	}

	if defaultProvider.input != input {
		t.Fatalf(
			"default provider input = %+v, expected %+v",
			defaultProvider.input,
			input,
		)
	}
}

func TestWhatsAppRouterUsesMostSpecificPrefix(
	t *testing.T,
) {
	generalProvider := &testWhatsAppProvider{}
	specificProvider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix: "+1",
				Provider:    generalProvider,
			},
			{
				PhonePrefix: "+1242",
				Provider:    specificProvider,
			},
		},
		nil,
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
			PhoneNumber: "+12425551234",
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

	if specificProvider.calls != 1 {
		t.Fatalf(
			"specific provider calls = %d, expected 1",
			specificProvider.calls,
		)
	}

	if generalProvider.calls != 0 {
		t.Fatalf(
			"general provider calls = %d, expected 0",
			generalProvider.calls,
		)
	}
}

func TestWhatsAppRouterRejectsDestinationWithoutProvider(
	t *testing.T,
) {
	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix: "+964",
				Provider:    &testWhatsAppProvider{},
			},
		},
		nil,
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
			"SendOTP() accepted destination without configured provider",
		)
	}
}

func TestWhatsAppRouterPropagatesProviderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"provider unavailable",
	)

	iraqProvider := &testWhatsAppProvider{
		err: expectedErr,
	}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		nil,
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

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"SendOTP() error = %v, expected wrapped provider error",
			err,
		)
	}
}

func TestWhatsAppRouterDoesNotFallbackAfterMatchedProviderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"regional provider delivery state unknown",
	)

	iraqProvider := &testWhatsAppProvider{
		err: expectedErr,
	}

	defaultProvider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		[]WhatsAppRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		defaultProvider,
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

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"SendOTP() error = %v, expected wrapped provider error",
			err,
		)
	}

	if iraqProvider.calls != 1 {
		t.Fatalf(
			"Iraq provider calls = %d, expected 1",
			iraqProvider.calls,
		)
	}

	if defaultProvider.calls != 0 {
		t.Fatalf(
			"default provider calls = %d, expected 0",
			defaultProvider.calls,
		)
	}
}

func TestWhatsAppRouterNormalizesDestinationPhoneNumber(
	t *testing.T,
) {
	provider := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		nil,
		provider,
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
			PhoneNumber: "  +9647501234567  ",
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

	if provider.input.PhoneNumber != "+9647501234567" {
		t.Fatalf(
			"provider phone number = %q, expected %q",
			provider.input.PhoneNumber,
			"+9647501234567",
		)
	}
}

func TestNewWhatsAppRouterRejectsInvalidRoutes(
	t *testing.T,
) {
	provider := &testWhatsAppProvider{}

	tests := []struct {
		name            string
		routes          []WhatsAppRoute
		defaultProvider WhatsAppProvider
	}{
		{
			name: "blank prefix",
			routes: []WhatsAppRoute{
				{
					PhonePrefix: " ",
					Provider:    provider,
				},
			},
		},
		{
			name: "non international prefix",
			routes: []WhatsAppRoute{
				{
					PhonePrefix: "964",
					Provider:    provider,
				},
			},
		},
		{
			name: "missing route provider",
			routes: []WhatsAppRoute{
				{
					PhonePrefix: "+964",
					Provider:    nil,
				},
			},
		},
		{
			name: "duplicate prefix",
			routes: []WhatsAppRoute{
				{
					PhonePrefix: "+964",
					Provider:    provider,
				},
				{
					PhonePrefix: " +964 ",
					Provider:    provider,
				},
			},
		},
		{
			name:            "no providers",
			routes:          nil,
			defaultProvider: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			router, err := NewWhatsAppRouter(
				testCase.routes,
				testCase.defaultProvider,
			)

			if err == nil {
				t.Fatal(
					"NewWhatsAppRouter() accepted invalid configuration",
				)
			}

			if router != nil {
				t.Fatal(
					"NewWhatsAppRouter() returned router for invalid configuration",
				)
			}
		})
	}
}
