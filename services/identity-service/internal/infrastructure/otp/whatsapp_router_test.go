package otp

import (
	"context"
	"errors"
	"testing"
	"time"

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

type whatsAppTrackingTestProvider struct {
	sendCalls        int
	trackedSendCalls int

	result WhatsAppProviderDeliveryResult
	err    error

	input WhatsAppOTPProviderInput
}

func (p *whatsAppTrackingTestProvider) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	p.sendCalls++
	p.input = input

	return p.err
}

func (p *whatsAppTrackingTestProvider) SendOTPTracked(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) (WhatsAppProviderDeliveryResult, error) {
	p.trackedSendCalls++
	p.input = input

	return p.result, p.err
}

type whatsAppTrackingTestStore struct {
	createCalls   int
	acceptedCalls int
	failedCalls   int
	unknownCalls  int

	createInput   DeliveryAttemptCreateInput
	acceptedInput DeliveryAttemptAcceptedInput
	failedInput   DeliveryAttemptFailedInput
	unknownInput  DeliveryAttemptUnknownInput

	attemptID string
}

func (s *whatsAppTrackingTestStore) CreateAttempt(
	ctx context.Context,
	input DeliveryAttemptCreateInput,
) (string, error) {
	s.createCalls++
	s.createInput = input

	if s.attemptID == "" {
		s.attemptID = "attempt-whatsapp-123"
	}

	return s.attemptID, nil
}

func (s *whatsAppTrackingTestStore) MarkAccepted(
	ctx context.Context,
	input DeliveryAttemptAcceptedInput,
) error {
	s.acceptedCalls++
	s.acceptedInput = input

	return nil
}

func (s *whatsAppTrackingTestStore) MarkFailed(
	ctx context.Context,
	input DeliveryAttemptFailedInput,
) error {
	s.failedCalls++
	s.failedInput = input

	return nil
}

func (s *whatsAppTrackingTestStore) MarkUnknown(
	ctx context.Context,
	input DeliveryAttemptUnknownInput,
) error {
	s.unknownCalls++
	s.unknownInput = input

	return nil
}

func TestWhatsAppRouterTracksAcceptedDelivery(
	t *testing.T,
) {
	provider := &whatsAppTrackingTestProvider{
		result: WhatsAppProviderDeliveryResult{
			ProviderMessageID: "request-whatsapp-123",
			ProviderStatus:    "accepted",
		},
	}

	store := &whatsAppTrackingTestStore{
		attemptID: "attempt-whatsapp-123",
	}

	router, err := NewWhatsAppRouter(
		nil,
		provider,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppDeliveryTrackingStore(
			store,
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
			ChallengeID: "challenge-whatsapp-123",
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

	if provider.trackedSendCalls != 1 {
		t.Fatalf(
			"tracked provider calls = %d, want 1",
			provider.trackedSendCalls,
		)
	}

	if provider.sendCalls != 0 {
		t.Fatalf(
			"legacy provider calls = %d, want 0",
			provider.sendCalls,
		)
	}

	if store.createCalls != 1 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 1",
			store.createCalls,
		)
	}

	if store.createInput.ChallengeID !=
		"challenge-whatsapp-123" {
		t.Fatalf(
			"challenge ID = %q, want %q",
			store.createInput.ChallengeID,
			"challenge-whatsapp-123",
		)
	}

	if store.createInput.Channel !=
		DeliveryTrackingChannelWhatsApp {
		t.Fatalf(
			"channel = %q, want %q",
			store.createInput.Channel,
			DeliveryTrackingChannelWhatsApp,
		)
	}

	if store.createInput.Provider !=
		DeliveryTrackingProvider("bulksmsiraq") {
		t.Fatalf(
			"provider = %q, want %q",
			store.createInput.Provider,
			"bulksmsiraq",
		)
	}

	if store.acceptedCalls != 1 {
		t.Fatalf(
			"MarkAccepted() calls = %d, want 1",
			store.acceptedCalls,
		)
	}

	if store.acceptedInput.AttemptID !=
		"attempt-whatsapp-123" {
		t.Fatalf(
			"accepted attempt ID = %q, want %q",
			store.acceptedInput.AttemptID,
			"attempt-whatsapp-123",
		)
	}

	if store.acceptedInput.ProviderMessageID !=
		"request-whatsapp-123" {
		t.Fatalf(
			"provider message ID = %q, want %q",
			store.acceptedInput.ProviderMessageID,
			"request-whatsapp-123",
		)
	}

	if store.failedCalls != 0 {
		t.Fatalf(
			"MarkFailed() calls = %d, want 0",
			store.failedCalls,
		)
	}

	if store.unknownCalls != 0 {
		t.Fatalf(
			"MarkUnknown() calls = %d, want 0",
			store.unknownCalls,
		)
	}
}

func TestWhatsAppRouterTracksFailedDelivery(
	t *testing.T,
) {
	tests := []struct {
		name string
		kind SMSProviderErrorKind
	}{
		{
			name: "permanent",
			kind: SMSProviderErrorPermanent,
		},
		{
			name: "rate limited",
			kind: SMSProviderErrorRateLimited,
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				provider := &whatsAppTrackingTestProvider{
					err: &SMSProviderError{
						Provider: "bulksmsiraq",
						Kind:     test.kind,
					},
				}

				store := &whatsAppTrackingTestStore{
					attemptID: "attempt-whatsapp-failed",
				}

				router, err := NewWhatsAppRouter(
					nil,
					provider,
					WithWhatsAppRouterDefaultProviderName(
						DeliveryMetricProvider(
							"bulksmsiraq",
						),
					),
					WithWhatsAppDeliveryTrackingStore(
						store,
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
						ChallengeID: "challenge-whatsapp-failed",
						PhoneNumber: "+9647501234567",
						Code:        "123456",
						Purpose:     auth.OTPPurposeLogin,
						Locale:      "ar",
					},
				)
				if err == nil {
					t.Fatal(
						"SendOTP() returned nil error",
					)
				}

				if store.createCalls != 1 {
					t.Fatalf(
						"CreateAttempt() calls = %d, want 1",
						store.createCalls,
					)
				}

				if store.failedCalls != 1 {
					t.Fatalf(
						"MarkFailed() calls = %d, want 1",
						store.failedCalls,
					)
				}

				if store.failedInput.FailureCode !=
					string(test.kind) {
					t.Fatalf(
						"failure code = %q, want %q",
						store.failedInput.FailureCode,
						test.kind,
					)
				}

				if store.acceptedCalls != 0 {
					t.Fatalf(
						"MarkAccepted() calls = %d, want 0",
						store.acceptedCalls,
					)
				}

				if store.unknownCalls != 0 {
					t.Fatalf(
						"MarkUnknown() calls = %d, want 0",
						store.unknownCalls,
					)
				}
			},
		)
	}
}

func TestWhatsAppRouterTracksUnknownDeliveryState(
	t *testing.T,
) {
	tests := []struct {
		name string
		kind SMSProviderErrorKind
	}{
		{
			name: "temporary",
			kind: SMSProviderErrorTemporary,
		},
		{
			name: "unknown delivery state",
			kind: SMSProviderErrorUnknownDeliveryState,
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				provider := &whatsAppTrackingTestProvider{
					err: &SMSProviderError{
						Provider: "bulksmsiraq",
						Kind:     test.kind,
					},
				}

				store := &whatsAppTrackingTestStore{
					attemptID: "attempt-whatsapp-unknown",
				}

				router, err := NewWhatsAppRouter(
					nil,
					provider,
					WithWhatsAppRouterDefaultProviderName(
						DeliveryMetricProvider(
							"bulksmsiraq",
						),
					),
					WithWhatsAppDeliveryTrackingStore(
						store,
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
						ChallengeID: "challenge-whatsapp-unknown",
						PhoneNumber: "+9647501234567",
						Code:        "123456",
						Purpose:     auth.OTPPurposeLogin,
						Locale:      "ku",
					},
				)
				if err == nil {
					t.Fatal(
						"SendOTP() returned nil error",
					)
				}

				if store.createCalls != 1 {
					t.Fatalf(
						"CreateAttempt() calls = %d, want 1",
						store.createCalls,
					)
				}

				if store.unknownCalls != 1 {
					t.Fatalf(
						"MarkUnknown() calls = %d, want 1",
						store.unknownCalls,
					)
				}

				if store.failedCalls != 0 {
					t.Fatalf(
						"MarkFailed() calls = %d, want 0",
						store.failedCalls,
					)
				}

				if store.acceptedCalls != 0 {
					t.Fatalf(
						"MarkAccepted() calls = %d, want 0",
						store.acceptedCalls,
					)
				}
			},
		)
	}
}

func TestWhatsAppRouterSkipsTrackingForLegacyProvider(
	t *testing.T,
) {
	provider := &testWhatsAppProvider{}

	store := &whatsAppTrackingTestStore{}

	router, err := NewWhatsAppRouter(
		nil,
		provider,
		WithWhatsAppDeliveryTrackingStore(
			store,
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
			ChallengeID: "challenge-whatsapp-legacy",
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

	if provider.calls != 1 {
		t.Fatalf(
			"legacy provider calls = %d, want 1",
			provider.calls,
		)
	}

	if store.createCalls != 0 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 0",
			store.createCalls,
		)
	}
}

func TestWhatsAppRouterSkipsTrackingWithoutChallengeID(
	t *testing.T,
) {
	provider := &whatsAppTrackingTestProvider{
		result: WhatsAppProviderDeliveryResult{
			ProviderMessageID: "request-not-used",
		},
	}

	store := &whatsAppTrackingTestStore{}

	router, err := NewWhatsAppRouter(
		nil,
		provider,
		WithWhatsAppDeliveryTrackingStore(
			store,
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
			Locale:      "en",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	if provider.sendCalls != 1 {
		t.Fatalf(
			"legacy SendOTP() calls = %d, want 1",
			provider.sendCalls,
		)
	}

	if provider.trackedSendCalls != 0 {
		t.Fatalf(
			"SendOTPTracked() calls = %d, want 0",
			provider.trackedSendCalls,
		)
	}

	if store.createCalls != 0 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 0",
			store.createCalls,
		)
	}
}

type whatsAppFailoverTrackingStore struct {
	createInputs   []DeliveryAttemptCreateInput
	acceptedInputs []DeliveryAttemptAcceptedInput
	failedInputs   []DeliveryAttemptFailedInput
	unknownInputs  []DeliveryAttemptUnknownInput

	attemptIDs []string
}

func (s *whatsAppFailoverTrackingStore) CreateAttempt(
	_ context.Context,
	input DeliveryAttemptCreateInput,
) (string, error) {
	s.createInputs = append(
		s.createInputs,
		input,
	)

	index := len(s.createInputs) - 1

	if index >= len(s.attemptIDs) {
		return "", errors.New(
			"no WhatsApp test attempt ID configured",
		)
	}

	return s.attemptIDs[index], nil
}

func (s *whatsAppFailoverTrackingStore) MarkAccepted(
	_ context.Context,
	input DeliveryAttemptAcceptedInput,
) error {
	s.acceptedInputs = append(
		s.acceptedInputs,
		input,
	)

	return nil
}

func (s *whatsAppFailoverTrackingStore) MarkFailed(
	_ context.Context,
	input DeliveryAttemptFailedInput,
) error {
	s.failedInputs = append(
		s.failedInputs,
		input,
	)

	return nil
}

func (s *whatsAppFailoverTrackingStore) MarkUnknown(
	_ context.Context,
	input DeliveryAttemptUnknownInput,
) error {
	s.unknownInputs = append(
		s.unknownInputs,
		input,
	)

	return nil
}

func TestWhatsAppRouterFailsOverAfterRateLimitedProvider(
	t *testing.T,
) {
	primary := &whatsAppTrackingTestProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorRateLimited,
		},
	}

	fallback := &whatsAppTrackingTestProvider{
		result: WhatsAppProviderDeliveryResult{
			ProviderMessageID: "meta-message-123",
			ProviderStatus:    "accepted",
		},
	}

	store := &whatsAppFailoverTrackingStore{
		attemptIDs: []string{
			"attempt-primary",
			"attempt-fallback",
		},
	}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppDeliveryTrackingStore(
			store,
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	input := WhatsAppOTPProviderInput{
		ChallengeID: "challenge-whatsapp-failover",
		PhoneNumber: "+9647501234567",
		Code:        "123456",
		Purpose:     auth.OTPPurposeLogin,
		Locale:      "ar",
	}

	if err := router.SendOTP(
		context.Background(),
		input,
	); err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	if primary.trackedSendCalls != 1 {
		t.Fatalf(
			"primary tracked calls = %d, want 1",
			primary.trackedSendCalls,
		)
	}

	if fallback.trackedSendCalls != 1 {
		t.Fatalf(
			"fallback tracked calls = %d, want 1",
			fallback.trackedSendCalls,
		)
	}

	if len(store.createInputs) != 2 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 2",
			len(store.createInputs),
		)
	}

	if store.createInputs[0].Provider !=
		DeliveryTrackingProviderBulkSMSIraq {
		t.Fatalf(
			"primary tracked provider = %q, want %q",
			store.createInputs[0].Provider,
			DeliveryTrackingProviderBulkSMSIraq,
		)
	}

	if store.createInputs[1].Provider !=
		DeliveryTrackingProviderMeta {
		t.Fatalf(
			"fallback tracked provider = %q, want %q",
			store.createInputs[1].Provider,
			DeliveryTrackingProviderMeta,
		)
	}

	if len(store.failedInputs) != 1 {
		t.Fatalf(
			"MarkFailed() calls = %d, want 1",
			len(store.failedInputs),
		)
	}

	if store.failedInputs[0].AttemptID !=
		"attempt-primary" {
		t.Fatalf(
			"failed attempt ID = %q, want %q",
			store.failedInputs[0].AttemptID,
			"attempt-primary",
		)
	}

	if store.failedInputs[0].FailureCode !=
		string(SMSProviderErrorRateLimited) {
		t.Fatalf(
			"failure code = %q, want %q",
			store.failedInputs[0].FailureCode,
			SMSProviderErrorRateLimited,
		)
	}

	if len(store.acceptedInputs) != 1 {
		t.Fatalf(
			"MarkAccepted() calls = %d, want 1",
			len(store.acceptedInputs),
		)
	}

	if store.acceptedInputs[0].AttemptID !=
		"attempt-fallback" {
		t.Fatalf(
			"accepted attempt ID = %q, want %q",
			store.acceptedInputs[0].AttemptID,
			"attempt-fallback",
		)
	}

	if store.acceptedInputs[0].ProviderMessageID !=
		"meta-message-123" {
		t.Fatalf(
			"fallback provider message ID = %q, want %q",
			store.acceptedInputs[0].ProviderMessageID,
			"meta-message-123",
		)
	}

	if len(store.unknownInputs) != 0 {
		t.Fatalf(
			"MarkUnknown() calls = %d, want 0",
			len(store.unknownInputs),
		)
	}
}

func TestWhatsAppRouterDoesNotUseFallbackAfterPrimarySuccess(
	t *testing.T,
) {
	primary := &testWhatsAppProvider{}
	fallback := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
	)
	if err != nil {
		t.Fatalf(
			"NewWhatsAppRouter() returned an error: %v",
			err,
		)
	}

	if err := router.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "ar",
		},
	); err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	if primary.calls != 1 {
		t.Fatalf(
			"primary calls = %d, want 1",
			primary.calls,
		)
	}

	if fallback.calls != 0 {
		t.Fatalf(
			"fallback calls = %d, want 0",
			fallback.calls,
		)
	}
}

func TestWhatsAppRouterDoesNotFailOverForUnsafeProviderErrors(
	t *testing.T,
) {
	tests := []struct {
		name string
		kind SMSProviderErrorKind
	}{
		{
			name: "permanent",
			kind: SMSProviderErrorPermanent,
		},
		{
			name: "temporary",
			kind: SMSProviderErrorTemporary,
		},
		{
			name: "unknown delivery state",
			kind: SMSProviderErrorUnknownDeliveryState,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				primary := &testWhatsAppProvider{
					err: &SMSProviderError{
						Provider: "bulksmsiraq",
						Kind:     testCase.kind,
					},
				}

				fallback := &testWhatsAppProvider{}

				router, err := NewWhatsAppRouter(
					nil,
					primary,
					WithWhatsAppRouterDefaultProviderName(
						DeliveryMetricProvider(
							"bulksmsiraq",
						),
					),
					WithWhatsAppFallbackProvider(
						DeliveryMetricProvider("meta"),
						fallback,
						ConservativeProviderFailoverPolicy{},
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
				if err == nil {
					t.Fatal(
						"SendOTP() returned nil error",
					)
				}

				if primary.calls != 1 {
					t.Fatalf(
						"primary calls = %d, want 1",
						primary.calls,
					)
				}

				if fallback.calls != 0 {
					t.Fatalf(
						"fallback calls = %d, want 0",
						fallback.calls,
					)
				}
			},
		)
	}
}

func TestWhatsAppRouterReturnsPrimaryAndFallbackErrors(
	t *testing.T,
) {
	primaryErr := &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorRateLimited,
	}

	fallbackErr := &SMSProviderError{
		Provider: "meta",
		Kind:     SMSProviderErrorTemporary,
	}

	primary := &testWhatsAppProvider{
		err: primaryErr,
	}

	fallback := &testWhatsAppProvider{
		err: fallbackErr,
	}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
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
	if err == nil {
		t.Fatal(
			"SendOTP() returned nil error",
		)
	}

	if !errors.Is(
		err,
		primaryErr,
	) {
		t.Fatalf(
			"SendOTP() error does not contain primary error: %v",
			err,
		)
	}

	if !errors.Is(
		err,
		fallbackErr,
	) {
		t.Fatalf(
			"SendOTP() error does not contain fallback error: %v",
			err,
		)
	}

	if primary.calls != 1 {
		t.Fatalf(
			"primary calls = %d, want 1",
			primary.calls,
		)
	}

	if fallback.calls != 1 {
		t.Fatalf(
			"fallback calls = %d, want 1",
			fallback.calls,
		)
	}
}

func TestWhatsAppRouterDoesNotRetrySameProviderAsFallback(
	t *testing.T,
) {
	primaryErr := &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorRateLimited,
	}

	primary := &testWhatsAppProvider{
		err: primaryErr,
	}

	fallback := &testWhatsAppProvider{}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider(" BULKSMSIRAQ "),
			fallback,
			ConservativeProviderFailoverPolicy{},
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
	if err == nil {
		t.Fatal(
			"SendOTP() returned nil error",
		)
	}

	if !errors.Is(
		err,
		primaryErr,
	) {
		t.Fatalf(
			"SendOTP() error = %v, want primary error",
			err,
		)
	}

	if primary.calls != 1 {
		t.Fatalf(
			"primary calls = %d, want 1",
			primary.calls,
		)
	}

	if fallback.calls != 0 {
		t.Fatalf(
			"fallback calls = %d, want 0",
			fallback.calls,
		)
	}
}
func TestWhatsAppRouterBypassesPrimaryWhenCircuitIsOpen(
	t *testing.T,
) {
	primary := &testWhatsAppProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorTemporary,
		},
	}

	fallback := &testWhatsAppProvider{}
	healthMetricsRecorder := &testSMSProviderHealthMetricsRecorder{}

	healthTracker, err :=
		NewCircuitBreakerProviderHealthTracker(
			2,
			time.Minute,
		)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	deliveryMetrics := &testDeliveryMetricsRecorder{}
	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithWhatsAppProviderHealthMetricsRecorder(healthMetricsRecorder),
		WithWhatsAppRouterDeliveryMetricsRecorder(deliveryMetrics),
		WithWhatsAppProviderHealthTracker(
			healthTracker,
		),
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

	for i := 0; i < 2; i++ {
		err := router.SendOTP(
			context.Background(),
			input,
		)

		if err == nil {
			t.Fatalf(
				"SendOTP() call %d returned nil error",
				i+1,
			)
		}
	}

	if primary.calls != 2 {
		t.Fatalf(
			"primary calls = %d, want 2",
			primary.calls,
		)
	}

	if fallback.calls != 0 {
		t.Fatalf(
			"fallback calls before circuit bypass = %d, want 0",
			fallback.calls,
		)
	}

	if healthMetricsRecorder.calls != 0 {
		t.Fatal("health metric emitted before a provider was skipped")
	}
	deliveryMetrics.deliveries = nil
	primary.err = nil

	if err := router.SendOTP(
		context.Background(),
		input,
	); err != nil {
		t.Fatalf(
			"SendOTP() through open-circuit fallback returned an error: %v",
			err,
		)
	}

	if primary.calls != 2 {
		t.Fatalf(
			"primary calls after circuit opened = %d, want 2",
			primary.calls,
		)
	}

	if fallback.calls != 1 {
		t.Fatalf(
			"fallback calls = %d, want 1",
			fallback.calls,
		)
	}

	if healthMetricsRecorder.calls != 1 ||
		healthMetricsRecorder.channel != DeliveryMetricChannelWhatsApp ||
		healthMetricsRecorder.provider != DeliveryMetricProviderBulkSMSIraq ||
		healthMetricsRecorder.event != ProviderHealthMetricEventCircuitOpen {
		t.Fatal("expected exactly one whatsapp/bulksmsiraq/circuit_open metric")
	}
	requireSingleDeliveryMetric(t, deliveryMetrics, DeliveryMetricChannelWhatsApp,
		DeliveryMetricProviderMeta, DeliveryMetricOutcomeSuccess)
}

func TestWhatsAppRouterUnknownDeliveryDoesNotFallbackForCurrentRequestButOpensCircuit(
	t *testing.T,
) {
	primary := &testWhatsAppProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorUnknownDeliveryState,
		},
	}

	fallback := &testWhatsAppProvider{}

	healthTracker, err :=
		NewCircuitBreakerProviderHealthTracker(
			1,
			time.Minute,
		)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithWhatsAppProviderHealthTracker(
			healthTracker,
		),
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
	if err == nil {
		t.Fatal(
			"first SendOTP() returned nil error",
		)
	}

	if primary.calls != 1 {
		t.Fatalf(
			"primary calls = %d, want 1",
			primary.calls,
		)
	}

	if fallback.calls != 0 {
		t.Fatalf(
			"fallback calls for unknown current delivery = %d, want 0",
			fallback.calls,
		)
	}

	primary.err = nil

	if err := router.SendOTP(
		context.Background(),
		input,
	); err != nil {
		t.Fatalf(
			"second SendOTP() returned an error: %v",
			err,
		)
	}

	if primary.calls != 1 {
		t.Fatalf(
			"primary calls after circuit opened = %d, want 1",
			primary.calls,
		)
	}

	if fallback.calls != 1 {
		t.Fatalf(
			"fallback calls for next request = %d, want 1",
			fallback.calls,
		)
	}
}

func TestWhatsAppRouterPermanentFailureDoesNotOpenCircuit(
	t *testing.T,
) {
	primary := &testWhatsAppProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorPermanent,
		},
	}

	fallback := &testWhatsAppProvider{}

	healthTracker, err :=
		NewCircuitBreakerProviderHealthTracker(
			1,
			time.Minute,
		)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithWhatsAppProviderHealthTracker(
			healthTracker,
		),
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

	for i := 0; i < 2; i++ {
		err := router.SendOTP(
			context.Background(),
			input,
		)

		if err == nil {
			t.Fatalf(
				"SendOTP() call %d returned nil error",
				i+1,
			)
		}
	}

	if primary.calls != 2 {
		t.Fatalf(
			"primary calls = %d, want 2",
			primary.calls,
		)
	}

	if fallback.calls != 0 {
		t.Fatalf(
			"fallback calls = %d, want 0",
			fallback.calls,
		)
	}
}

func TestWhatsAppRouterProviderSuccessResetsCircuitFailureCount(
	t *testing.T,
) {
	primary := &testWhatsAppProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorTemporary,
		},
	}

	fallback := &testWhatsAppProvider{}

	healthTracker, err :=
		NewCircuitBreakerProviderHealthTracker(
			2,
			time.Minute,
		)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	router, err := NewWhatsAppRouter(
		nil,
		primary,
		WithWhatsAppRouterDefaultProviderName(
			DeliveryMetricProvider("bulksmsiraq"),
		),
		WithWhatsAppFallbackProvider(
			DeliveryMetricProvider("meta"),
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithWhatsAppProviderHealthTracker(
			healthTracker,
		),
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

	if err := router.SendOTP(
		context.Background(),
		input,
	); err == nil {
		t.Fatal(
			"first SendOTP() returned nil error",
		)
	}

	primary.err = nil

	if err := router.SendOTP(
		context.Background(),
		input,
	); err != nil {
		t.Fatalf(
			"successful SendOTP() returned an error: %v",
			err,
		)
	}

	primary.err = &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorTemporary,
	}

	if err := router.SendOTP(
		context.Background(),
		input,
	); err == nil {
		t.Fatal(
			"third SendOTP() returned nil error",
		)
	}

	primary.err = nil

	if err := router.SendOTP(
		context.Background(),
		input,
	); err != nil {
		t.Fatalf(
			"fourth SendOTP() returned an error: %v",
			err,
		)
	}

	if primary.calls != 4 {
		t.Fatalf(
			"primary calls = %d, want 4",
			primary.calls,
		)
	}

	if fallback.calls != 0 {
		t.Fatalf(
			"fallback calls = %d, want 0",
			fallback.calls,
		)
	}
}
