package otp

import (
	"context"
	"errors"
	"testing"
)

func TestSMSRouterRoutesByPhonePrefix(
	t *testing.T,
) {
	iraqProvider := &testSMSProvider{}
	defaultProvider := &testSMSProvider{}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		defaultProvider,
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	message := SMSMessage{
		To:   "+9647501234567",
		Body: "Your verification code is 123456",
	}

	err = router.Send(
		context.Background(),
		message,
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
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

	if iraqProvider.message != message {
		t.Fatalf(
			"Iraq provider message = %+v, expected %+v",
			iraqProvider.message,
			message,
		)
	}
}

func TestSMSRouterUsesDefaultProvider(
	t *testing.T,
) {
	iraqProvider := &testSMSProvider{}
	defaultProvider := &testSMSProvider{}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		defaultProvider,
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	message := SMSMessage{
		To:   "+971501234567",
		Body: "Your verification code is 123456",
	}

	err = router.Send(
		context.Background(),
		message,
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
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
}

func TestSMSRouterUsesMostSpecificPrefix(
	t *testing.T,
) {
	generalProvider := &testSMSProvider{}
	specificProvider := &testSMSProvider{}

	router, err := NewSMSRouter(
		[]SMSRoute{
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
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+12425551234",
			Body: "Your verification code is 123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
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

func TestSMSRouterRejectsDestinationWithoutProvider(
	t *testing.T,
) {
	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix: "+964",
				Provider:    &testSMSProvider{},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+971501234567",
			Body: "Your verification code is 123456",
		},
	)

	if err == nil {
		t.Fatal(
			"Send() accepted destination without configured provider",
		)
	}
}

func TestSMSRouterPropagatesProviderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"provider unavailable",
	)

	iraqProvider := &testSMSProvider{
		err: expectedErr,
	}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix: "+964",
				Provider:    iraqProvider,
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Send() error = %v, expected wrapped provider error",
			err,
		)
	}
}

func TestNewSMSRouterRejectsInvalidRoutes(
	t *testing.T,
) {
	provider := &testSMSProvider{}

	tests := []struct {
		name            string
		routes          []SMSRoute
		defaultProvider SMSProvider
	}{
		{
			name: "blank prefix",
			routes: []SMSRoute{
				{
					PhonePrefix: " ",
					Provider:    provider,
				},
			},
		},
		{
			name: "non international prefix",
			routes: []SMSRoute{
				{
					PhonePrefix: "964",
					Provider:    provider,
				},
			},
		},
		{
			name: "missing route provider",
			routes: []SMSRoute{
				{
					PhonePrefix: "+964",
					Provider:    nil,
				},
			},
		},
		{
			name: "duplicate prefix",
			routes: []SMSRoute{
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
			router, err := NewSMSRouter(
				testCase.routes,
				testCase.defaultProvider,
			)

			if err == nil {
				t.Fatal(
					"NewSMSRouter() accepted invalid configuration",
				)
			}

			if router != nil {
				t.Fatal(
					"NewSMSRouter() returned router for invalid configuration",
				)
			}
		})
	}
}
