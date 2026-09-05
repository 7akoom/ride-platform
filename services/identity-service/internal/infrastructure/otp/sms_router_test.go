package otp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSMSRouterRoutesByPhonePrefix(
	t *testing.T,
) {
	iraqProvider := &testSMSProvider{}
	defaultProvider := &testSMSProvider{}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     iraqProvider,
			},
		},
		"telnyx",
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
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     iraqProvider,
			},
		},
		"telnyx",
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
				PhonePrefix:  "+1",
				ProviderName: "telnyx",
				Provider:     generalProvider,
			},
			{
				PhonePrefix:  "+1242",
				ProviderName: "bulksmsiraq",
				Provider:     specificProvider,
			},
		},
		"",
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
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     &testSMSProvider{},
			},
		},
		"",
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
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     iraqProvider,
			},
		},
		"",
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
		name                string
		routes              []SMSRoute
		defaultProviderName string
		defaultProvider     SMSProvider
	}{
		{
			name: "blank prefix",
			routes: []SMSRoute{
				{
					PhonePrefix:  " ",
					ProviderName: "bulksmsiraq",
					Provider:     provider,
				},
			},
		},
		{
			name: "non international prefix",
			routes: []SMSRoute{
				{
					PhonePrefix:  "964",
					ProviderName: "bulksmsiraq",
					Provider:     provider,
				},
			},
		},
		{
			name: "missing route provider name",
			routes: []SMSRoute{
				{
					PhonePrefix:  "+964",
					ProviderName: " ",
					Provider:     provider,
				},
			},
		},
		{
			name: "missing route provider",
			routes: []SMSRoute{
				{
					PhonePrefix:  "+964",
					ProviderName: "bulksmsiraq",
					Provider:     nil,
				},
			},
		},
		{
			name: "duplicate prefix",
			routes: []SMSRoute{
				{
					PhonePrefix:  "+964",
					ProviderName: "bulksmsiraq",
					Provider:     provider,
				},
				{
					PhonePrefix:  " +964 ",
					ProviderName: "telnyx",
					Provider:     provider,
				},
			},
		},
		{
			name:                "missing default provider name",
			routes:              nil,
			defaultProviderName: " ",
			defaultProvider:     provider,
		},
		{
			name:                "default provider name without provider",
			routes:              nil,
			defaultProviderName: "telnyx",
			defaultProvider:     nil,
		},
		{
			name:                "no providers",
			routes:              nil,
			defaultProviderName: "",
			defaultProvider:     nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			router, err := NewSMSRouter(
				testCase.routes,
				testCase.defaultProviderName,
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

type testTrackedSMSProvider struct {
	sendCalls        int
	sendTrackedCalls int
	message          SMSMessage
	result           SMSProviderDeliveryResult
	err              error
}

type testSMSProviderHealthMetricsRecorder struct {
	calls    int
	channel  DeliveryMetricChannel
	provider DeliveryMetricProvider
	event    ProviderHealthMetricEvent
}

func (r *testSMSProviderHealthMetricsRecorder) RecordOTPProviderHealthEvent(
	_ context.Context,
	channel DeliveryMetricChannel,
	provider DeliveryMetricProvider,
	event ProviderHealthMetricEvent,
) {
	r.calls++
	r.channel = channel
	r.provider = provider
	r.event = event
}

func (p *testTrackedSMSProvider) Send(
	context.Context,
	SMSMessage,
) error {
	p.sendCalls++
	return p.err
}

func (p *testTrackedSMSProvider) SendTracked(
	_ context.Context,
	message SMSMessage,
) (SMSProviderDeliveryResult, error) {
	p.sendTrackedCalls++
	p.message = message

	return p.result, p.err
}

type testSMSDeliveryTrackingStore struct {
	createCalls       int
	markAcceptedCalls int
	markFailedCalls   int
	markUnknownCalls  int

	createInput       DeliveryAttemptCreateInput
	markAcceptedInput DeliveryAttemptAcceptedInput
	markFailedInput   DeliveryAttemptFailedInput
	markUnknownInput  DeliveryAttemptUnknownInput

	attemptID string
	createErr error
}

func (s *testSMSDeliveryTrackingStore) CreateAttempt(
	_ context.Context,
	input DeliveryAttemptCreateInput,
) (string, error) {
	s.createCalls++
	s.createInput = input

	return s.attemptID, s.createErr
}

func (s *testSMSDeliveryTrackingStore) MarkAccepted(
	_ context.Context,
	input DeliveryAttemptAcceptedInput,
) error {
	s.markAcceptedCalls++
	s.markAcceptedInput = input

	return nil
}

func (s *testSMSDeliveryTrackingStore) MarkFailed(
	_ context.Context,
	input DeliveryAttemptFailedInput,
) error {
	s.markFailedCalls++
	s.markFailedInput = input

	return nil
}

func (s *testSMSDeliveryTrackingStore) MarkUnknown(
	_ context.Context,
	input DeliveryAttemptUnknownInput,
) error {
	s.markUnknownCalls++
	s.markUnknownInput = input

	return nil
}
func TestSMSRouterTracksAcceptedDelivery(
	t *testing.T,
) {
	provider := &testTrackedSMSProvider{
		result: SMSProviderDeliveryResult{
			ProviderMessageID: "message-123",
			ProviderStatus:    "queued",
		},
	}

	trackingStore := &testSMSDeliveryTrackingStore{
		attemptID: "attempt-123",
	}

	router, err := NewSMSRouter(
		nil,
		"telnyx",
		provider,
		WithSMSDeliveryTrackingStore(
			trackingStore,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	message := SMSMessage{
		ChallengeID: "otp_ch_tracking_success",
		To:          "+971501234567",
		Body:        "Your verification code is 123456",
	}

	if err := router.Send(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if provider.sendCalls != 0 {
		t.Fatalf(
			"legacy Send() calls = %d, want 0",
			provider.sendCalls,
		)
	}

	if provider.sendTrackedCalls != 1 {
		t.Fatalf(
			"SendTracked() calls = %d, want 1",
			provider.sendTrackedCalls,
		)
	}

	if provider.message != message {
		t.Fatalf(
			"tracked provider message = %+v, want %+v",
			provider.message,
			message,
		)
	}

	if trackingStore.createCalls != 1 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 1",
			trackingStore.createCalls,
		)
	}

	if trackingStore.createInput.ChallengeID !=
		message.ChallengeID {
		t.Fatalf(
			"tracked challenge ID = %q, want %q",
			trackingStore.createInput.ChallengeID,
			message.ChallengeID,
		)
	}

	if trackingStore.createInput.Channel !=
		DeliveryTrackingChannelSMS {
		t.Fatalf(
			"tracked channel = %q, want %q",
			trackingStore.createInput.Channel,
			DeliveryTrackingChannelSMS,
		)
	}

	if trackingStore.createInput.Provider !=
		DeliveryTrackingProviderTelnyx {
		t.Fatalf(
			"tracked provider = %q, want %q",
			trackingStore.createInput.Provider,
			DeliveryTrackingProviderTelnyx,
		)
	}

	if trackingStore.createInput.AttemptedAt.IsZero() {
		t.Fatal(
			"tracked attempted time is zero",
		)
	}

	if trackingStore.markAcceptedCalls != 1 {
		t.Fatalf(
			"MarkAccepted() calls = %d, want 1",
			trackingStore.markAcceptedCalls,
		)
	}

	if trackingStore.markAcceptedInput.AttemptID !=
		"attempt-123" {
		t.Fatalf(
			"accepted attempt ID = %q, want %q",
			trackingStore.markAcceptedInput.AttemptID,
			"attempt-123",
		)
	}

	if trackingStore.markAcceptedInput.ProviderMessageID !=
		"message-123" {
		t.Fatalf(
			"provider message ID = %q, want %q",
			trackingStore.markAcceptedInput.ProviderMessageID,
			"message-123",
		)
	}

	if trackingStore.markAcceptedInput.ProviderStatus !=
		"queued" {
		t.Fatalf(
			"provider status = %q, want %q",
			trackingStore.markAcceptedInput.ProviderStatus,
			"queued",
		)
	}

	if trackingStore.markAcceptedInput.AcceptedAt.IsZero() {
		t.Fatal(
			"accepted time is zero",
		)
	}

	if trackingStore.markFailedCalls != 0 {
		t.Fatalf(
			"MarkFailed() calls = %d, want 0",
			trackingStore.markFailedCalls,
		)
	}

	if trackingStore.markUnknownCalls != 0 {
		t.Fatalf(
			"MarkUnknown() calls = %d, want 0",
			trackingStore.markUnknownCalls,
		)
	}
}
func TestSMSRouterTracksFailedDelivery(
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &testTrackedSMSProvider{
				err: &SMSProviderError{
					Provider: "telnyx",
					Kind:     testCase.kind,
				},
			}

			trackingStore := &testSMSDeliveryTrackingStore{
				attemptID: "attempt-failed-123",
			}

			router, err := NewSMSRouter(
				nil,
				"telnyx",
				provider,
				WithSMSDeliveryTrackingStore(
					trackingStore,
				),
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
					ChallengeID: "otp_ch_tracking_failed",
					To:          "+971501234567",
					Body:        "Your verification code is 123456",
				},
			)
			if err == nil {
				t.Fatal(
					"Send() returned nil error",
				)
			}

			if provider.sendTrackedCalls != 1 {
				t.Fatalf(
					"SendTracked() calls = %d, want 1",
					provider.sendTrackedCalls,
				)
			}

			if trackingStore.createCalls != 1 {
				t.Fatalf(
					"CreateAttempt() calls = %d, want 1",
					trackingStore.createCalls,
				)
			}

			if trackingStore.markFailedCalls != 1 {
				t.Fatalf(
					"MarkFailed() calls = %d, want 1",
					trackingStore.markFailedCalls,
				)
			}

			if trackingStore.markFailedInput.AttemptID !=
				"attempt-failed-123" {
				t.Fatalf(
					"failed attempt ID = %q, want %q",
					trackingStore.markFailedInput.AttemptID,
					"attempt-failed-123",
				)
			}

			if trackingStore.markFailedInput.FailureCode !=
				string(testCase.kind) {
				t.Fatalf(
					"failure code = %q, want %q",
					trackingStore.markFailedInput.FailureCode,
					testCase.kind,
				)
			}

			if trackingStore.markFailedInput.FailedAt.IsZero() {
				t.Fatal(
					"failed time is zero",
				)
			}

			if trackingStore.markAcceptedCalls != 0 {
				t.Fatalf(
					"MarkAccepted() calls = %d, want 0",
					trackingStore.markAcceptedCalls,
				)
			}

			if trackingStore.markUnknownCalls != 0 {
				t.Fatalf(
					"MarkUnknown() calls = %d, want 0",
					trackingStore.markUnknownCalls,
				)
			}
		})
	}
}
func TestSMSRouterTracksUnknownDeliveryState(
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &testTrackedSMSProvider{
				err: &SMSProviderError{
					Provider: "telnyx",
					Kind:     testCase.kind,
				},
			}

			trackingStore := &testSMSDeliveryTrackingStore{
				attemptID: "attempt-unknown-123",
			}

			router, err := NewSMSRouter(
				nil,
				"telnyx",
				provider,
				WithSMSDeliveryTrackingStore(
					trackingStore,
				),
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
					ChallengeID: "otp_ch_tracking_unknown",
					To:          "+971501234567",
					Body:        "Your verification code is 123456",
				},
			)
			if err == nil {
				t.Fatal(
					"Send() returned nil error",
				)
			}

			if provider.sendTrackedCalls != 1 {
				t.Fatalf(
					"SendTracked() calls = %d, want 1",
					provider.sendTrackedCalls,
				)
			}

			if trackingStore.createCalls != 1 {
				t.Fatalf(
					"CreateAttempt() calls = %d, want 1",
					trackingStore.createCalls,
				)
			}

			if trackingStore.markUnknownCalls != 1 {
				t.Fatalf(
					"MarkUnknown() calls = %d, want 1",
					trackingStore.markUnknownCalls,
				)
			}

			if trackingStore.markUnknownInput.AttemptID !=
				"attempt-unknown-123" {
				t.Fatalf(
					"unknown attempt ID = %q, want %q",
					trackingStore.markUnknownInput.AttemptID,
					"attempt-unknown-123",
				)
			}

			if trackingStore.markAcceptedCalls != 0 {
				t.Fatalf(
					"MarkAccepted() calls = %d, want 0",
					trackingStore.markAcceptedCalls,
				)
			}

			if trackingStore.markFailedCalls != 0 {
				t.Fatalf(
					"MarkFailed() calls = %d, want 0",
					trackingStore.markFailedCalls,
				)
			}
		})
	}
}
func TestSMSRouterSkipsTrackingForLegacyProvider(
	t *testing.T,
) {
	provider := &testSMSProvider{}

	trackingStore := &testSMSDeliveryTrackingStore{
		attemptID: "unused-attempt",
	}

	router, err := NewSMSRouter(
		nil,
		"telnyx",
		provider,
		WithSMSDeliveryTrackingStore(
			trackingStore,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	if err := router.Send(
		context.Background(),
		SMSMessage{
			ChallengeID: "otp_ch_legacy_provider",
			To:          "+971501234567",
			Body:        "Your verification code is 123456",
		},
	); err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if provider.calls != 1 {
		t.Fatalf(
			"legacy provider calls = %d, want 1",
			provider.calls,
		)
	}

	if trackingStore.createCalls != 0 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 0",
			trackingStore.createCalls,
		)
	}

	if trackingStore.markAcceptedCalls != 0 {
		t.Fatalf(
			"MarkAccepted() calls = %d, want 0",
			trackingStore.markAcceptedCalls,
		)
	}

	if trackingStore.markFailedCalls != 0 {
		t.Fatalf(
			"MarkFailed() calls = %d, want 0",
			trackingStore.markFailedCalls,
		)
	}

	if trackingStore.markUnknownCalls != 0 {
		t.Fatalf(
			"MarkUnknown() calls = %d, want 0",
			trackingStore.markUnknownCalls,
		)
	}
}

func TestSMSRouterSkipsTrackingWithoutChallengeID(
	t *testing.T,
) {
	provider := &testTrackedSMSProvider{
		result: SMSProviderDeliveryResult{
			ProviderMessageID: "message-unused",
			ProviderStatus:    "queued",
		},
	}

	trackingStore := &testSMSDeliveryTrackingStore{
		attemptID: "unused-attempt",
	}

	router, err := NewSMSRouter(
		nil,
		"telnyx",
		provider,
		WithSMSDeliveryTrackingStore(
			trackingStore,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	if err := router.Send(
		context.Background(),
		SMSMessage{
			To:   "+971501234567",
			Body: "Your verification code is 123456",
		},
	); err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if provider.sendCalls != 1 {
		t.Fatalf(
			"legacy Send() calls = %d, want 1",
			provider.sendCalls,
		)
	}

	if provider.sendTrackedCalls != 0 {
		t.Fatalf(
			"SendTracked() calls = %d, want 0",
			provider.sendTrackedCalls,
		)
	}

	if trackingStore.createCalls != 0 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 0",
			trackingStore.createCalls,
		)
	}

	if trackingStore.markAcceptedCalls != 0 {
		t.Fatalf(
			"MarkAccepted() calls = %d, want 0",
			trackingStore.markAcceptedCalls,
		)
	}

	if trackingStore.markFailedCalls != 0 {
		t.Fatalf(
			"MarkFailed() calls = %d, want 0",
			trackingStore.markFailedCalls,
		)
	}

	if trackingStore.markUnknownCalls != 0 {
		t.Fatalf(
			"MarkUnknown() calls = %d, want 0",
			trackingStore.markUnknownCalls,
		)
	}
}

type testSMSFailoverTrackingStore struct {
	createInputs       []DeliveryAttemptCreateInput
	markAcceptedInputs []DeliveryAttemptAcceptedInput
	markFailedInputs   []DeliveryAttemptFailedInput
	markUnknownInputs  []DeliveryAttemptUnknownInput

	attemptIDs []string
}

func (s *testSMSFailoverTrackingStore) CreateAttempt(
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
			"no test attempt ID configured",
		)
	}

	return s.attemptIDs[index], nil
}

func (s *testSMSFailoverTrackingStore) MarkAccepted(
	_ context.Context,
	input DeliveryAttemptAcceptedInput,
) error {
	s.markAcceptedInputs = append(
		s.markAcceptedInputs,
		input,
	)

	return nil
}

func (s *testSMSFailoverTrackingStore) MarkFailed(
	_ context.Context,
	input DeliveryAttemptFailedInput,
) error {
	s.markFailedInputs = append(
		s.markFailedInputs,
		input,
	)

	return nil
}

func (s *testSMSFailoverTrackingStore) MarkUnknown(
	_ context.Context,
	input DeliveryAttemptUnknownInput,
) error {
	s.markUnknownInputs = append(
		s.markUnknownInputs,
		input,
	)

	return nil
}

func TestSMSRouterFailsOverAfterRateLimitedProvider(
	t *testing.T,
) {
	primary := &testTrackedSMSProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorRateLimited,
		},
	}

	fallback := &testTrackedSMSProvider{
		result: SMSProviderDeliveryResult{
			ProviderMessageID: "telnyx-message-123",
			ProviderStatus:    "queued",
		},
	}

	trackingStore := &testSMSFailoverTrackingStore{
		attemptIDs: []string{
			"attempt-primary",
			"attempt-fallback",
		},
	}

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSDeliveryTrackingStore(
			trackingStore,
		),
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	message := SMSMessage{
		ChallengeID: "otp_ch_sms_failover",
		To:          "+9647501234567",
		Body:        "Your verification code is 123456",
		Code:        "123456",
		Locale:      "ar",
	}

	if err := router.Send(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if primary.sendTrackedCalls != 1 {
		t.Fatalf(
			"primary SendTracked() calls = %d, want 1",
			primary.sendTrackedCalls,
		)
	}

	if fallback.sendTrackedCalls != 1 {
		t.Fatalf(
			"fallback SendTracked() calls = %d, want 1",
			fallback.sendTrackedCalls,
		)
	}

	if len(trackingStore.createInputs) != 2 {
		t.Fatalf(
			"CreateAttempt() calls = %d, want 2",
			len(trackingStore.createInputs),
		)
	}

	if trackingStore.createInputs[0].Provider !=
		DeliveryTrackingProviderBulkSMSIraq {
		t.Fatalf(
			"primary tracked provider = %q, want %q",
			trackingStore.createInputs[0].Provider,
			DeliveryTrackingProviderBulkSMSIraq,
		)
	}

	if trackingStore.createInputs[1].Provider !=
		DeliveryTrackingProviderTelnyx {
		t.Fatalf(
			"fallback tracked provider = %q, want %q",
			trackingStore.createInputs[1].Provider,
			DeliveryTrackingProviderTelnyx,
		)
	}

	if len(trackingStore.markFailedInputs) != 1 {
		t.Fatalf(
			"MarkFailed() calls = %d, want 1",
			len(trackingStore.markFailedInputs),
		)
	}

	if trackingStore.markFailedInputs[0].AttemptID !=
		"attempt-primary" {
		t.Fatalf(
			"failed attempt ID = %q, want %q",
			trackingStore.markFailedInputs[0].AttemptID,
			"attempt-primary",
		)
	}

	if trackingStore.markFailedInputs[0].FailureCode !=
		string(SMSProviderErrorRateLimited) {
		t.Fatalf(
			"primary failure code = %q, want %q",
			trackingStore.markFailedInputs[0].FailureCode,
			SMSProviderErrorRateLimited,
		)
	}

	if len(trackingStore.markAcceptedInputs) != 1 {
		t.Fatalf(
			"MarkAccepted() calls = %d, want 1",
			len(trackingStore.markAcceptedInputs),
		)
	}

	if trackingStore.markAcceptedInputs[0].AttemptID !=
		"attempt-fallback" {
		t.Fatalf(
			"accepted attempt ID = %q, want %q",
			trackingStore.markAcceptedInputs[0].AttemptID,
			"attempt-fallback",
		)
	}

	if trackingStore.markAcceptedInputs[0].ProviderMessageID !=
		"telnyx-message-123" {
		t.Fatalf(
			"fallback provider message ID = %q, want %q",
			trackingStore.markAcceptedInputs[0].ProviderMessageID,
			"telnyx-message-123",
		)
	}

	if len(trackingStore.markUnknownInputs) != 0 {
		t.Fatalf(
			"MarkUnknown() calls = %d, want 0",
			len(trackingStore.markUnknownInputs),
		)
	}
}

func TestSMSRouterDoesNotUseFallbackAfterPrimarySuccess(
	t *testing.T,
) {
	primary := &testTrackedSMSProvider{}
	fallback := &testTrackedSMSProvider{}

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	if err := router.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	); err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if primary.sendCalls != 1 {
		t.Fatalf(
			"primary Send() calls = %d, want 1",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 0 {
		t.Fatalf(
			"fallback Send() calls = %d, want 0",
			fallback.sendCalls,
		)
	}
}

func TestSMSRouterDoesNotFailOverForUnsafeProviderErrors(
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
				primary := &testTrackedSMSProvider{
					err: &SMSProviderError{
						Provider: "bulksmsiraq",
						Kind:     testCase.kind,
					},
				}

				fallback := &testTrackedSMSProvider{}

				router, err := NewSMSRouter(
					nil,
					"bulksmsiraq",
					primary,
					WithSMSFallbackProvider(
						"telnyx",
						fallback,
						ConservativeProviderFailoverPolicy{},
					),
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
				if err == nil {
					t.Fatal(
						"Send() returned nil error",
					)
				}

				if primary.sendCalls != 1 {
					t.Fatalf(
						"primary Send() calls = %d, want 1",
						primary.sendCalls,
					)
				}

				if fallback.sendCalls != 0 {
					t.Fatalf(
						"fallback Send() calls = %d, want 0",
						fallback.sendCalls,
					)
				}
			},
		)
	}
}

func TestSMSRouterReturnsPrimaryAndFallbackErrors(
	t *testing.T,
) {
	primaryErr := &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorRateLimited,
	}

	fallbackErr := &SMSProviderError{
		Provider: "telnyx",
		Kind:     SMSProviderErrorTemporary,
	}

	primary := &testTrackedSMSProvider{
		err: primaryErr,
	}

	fallback := &testTrackedSMSProvider{
		err: fallbackErr,
	}

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
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
	if err == nil {
		t.Fatal(
			"Send() returned nil error",
		)
	}

	if !errors.Is(
		err,
		primaryErr,
	) {
		t.Fatalf(
			"Send() error does not contain primary error: %v",
			err,
		)
	}

	if !errors.Is(
		err,
		fallbackErr,
	) {
		t.Fatalf(
			"Send() error does not contain fallback error: %v",
			err,
		)
	}

	if primary.sendCalls != 1 {
		t.Fatalf(
			"primary Send() calls = %d, want 1",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 1 {
		t.Fatalf(
			"fallback Send() calls = %d, want 1",
			fallback.sendCalls,
		)
	}
}

func TestSMSRouterDoesNotRetrySameProviderAsFallback(
	t *testing.T,
) {
	primaryErr := &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorRateLimited,
	}

	primary := &testTrackedSMSProvider{
		err: primaryErr,
	}

	fallback := &testTrackedSMSProvider{}

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			" BULKSMSIRAQ ",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
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
	if err == nil {
		t.Fatal(
			"Send() returned nil error",
		)
	}

	if !errors.Is(
		err,
		primaryErr,
	) {
		t.Fatalf(
			"Send() error = %v, want primary error",
			err,
		)
	}

	if primary.sendCalls != 1 {
		t.Fatalf(
			"primary Send() calls = %d, want 1",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 0 {
		t.Fatalf(
			"fallback Send() calls = %d, want 0",
			fallback.sendCalls,
		)
	}
}
func TestSMSRouterBypassesPrimaryWhenCircuitIsOpen(
	t *testing.T,
) {
	primary := &testTrackedSMSProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorTemporary,
		},
	}

	fallback := &testTrackedSMSProvider{}

	healthTracker, err :=
		NewCircuitBreakerProviderHealthTracker(
			2,
			time.Minute,
		)

	healthMetricsRecorder :=
		&testSMSProviderHealthMetricsRecorder{}
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	deliveryMetrics := &testDeliveryMetricsRecorder{}
	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithSMSDeliveryMetricsRecorder(deliveryMetrics),
		WithSMSProviderHealthTracker(
			healthTracker,
		),
		WithSMSProviderHealthMetricsRecorder(
			healthMetricsRecorder,
		),
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

	for i := 0; i < 2; i++ {
		err := router.Send(
			context.Background(),
			message,
		)

		if err == nil {
			t.Fatalf(
				"Send() call %d returned nil error",
				i+1,
			)
		}
	}

	if primary.sendCalls != 2 {
		t.Fatalf(
			"primary Send() calls = %d, want 2",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 0 {
		t.Fatalf(
			"fallback Send() calls = %d before circuit bypass, want 0",
			fallback.sendCalls,
		)
	}

	if healthMetricsRecorder.calls != 0 {
		t.Fatal("health metric emitted before a provider was skipped")
	}
	deliveryMetrics.deliveries = nil
	primary.err = nil

	if err := router.Send(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf(
			"Send() through open-circuit fallback returned an error: %v",
			err,
		)
	}

	if primary.sendCalls != 2 {
		t.Fatalf(
			"primary Send() calls = %d after circuit opened, want 2",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 1 {
		t.Fatalf(
			"fallback Send() calls = %d, want 1",
			fallback.sendCalls,
		)
	}

	if healthMetricsRecorder.calls != 1 {
		t.Fatalf(
			"provider health metric calls = %d, want 1",
			healthMetricsRecorder.calls,
		)
	}

	if healthMetricsRecorder.channel !=
		DeliveryMetricChannelSMS {
		t.Fatalf(
			"provider health metric channel = %q, want %q",
			healthMetricsRecorder.channel,
			DeliveryMetricChannelSMS,
		)
	}

	if healthMetricsRecorder.provider !=
		DeliveryMetricProviderBulkSMSIraq {
		t.Fatalf(
			"provider health metric provider = %q, want %q",
			healthMetricsRecorder.provider,
			DeliveryMetricProviderBulkSMSIraq,
		)
	}

	if healthMetricsRecorder.event !=
		ProviderHealthMetricEventCircuitOpen {
		t.Fatalf(
			"provider health metric event = %q, want %q",
			healthMetricsRecorder.event,
			ProviderHealthMetricEventCircuitOpen,
		)
	}
	requireSingleDeliveryMetric(t, deliveryMetrics, DeliveryMetricChannelSMS,
		DeliveryMetricProviderTelnyx, DeliveryMetricOutcomeSuccess)
}

func TestSMSRouterUnknownDeliveryDoesNotFallbackForCurrentRequestButOpensCircuit(
	t *testing.T,
) {
	primary := &testTrackedSMSProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorUnknownDeliveryState,
		},
	}

	fallback := &testTrackedSMSProvider{}

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

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithSMSProviderHealthTracker(
			healthTracker,
		),
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
	if err == nil {
		t.Fatal(
			"first Send() returned nil error",
		)
	}

	if primary.sendCalls != 1 {
		t.Fatalf(
			"primary Send() calls = %d, want 1",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 0 {
		t.Fatalf(
			"fallback Send() calls for unknown current delivery = %d, want 0",
			fallback.sendCalls,
		)
	}

	primary.err = nil

	if err := router.Send(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf(
			"second Send() returned an error: %v",
			err,
		)
	}

	if primary.sendCalls != 1 {
		t.Fatalf(
			"primary Send() calls after circuit opened = %d, want 1",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 1 {
		t.Fatalf(
			"fallback Send() calls for next request = %d, want 1",
			fallback.sendCalls,
		)
	}
}

func TestSMSRouterPermanentFailureDoesNotOpenCircuit(
	t *testing.T,
) {
	primary := &testTrackedSMSProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorPermanent,
		},
	}

	fallback := &testTrackedSMSProvider{}

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

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithSMSProviderHealthTracker(
			healthTracker,
		),
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

	for i := 0; i < 2; i++ {
		err := router.Send(
			context.Background(),
			message,
		)

		if err == nil {
			t.Fatalf(
				"Send() call %d returned nil error",
				i+1,
			)
		}
	}

	if primary.sendCalls != 2 {
		t.Fatalf(
			"primary Send() calls = %d, want 2",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 0 {
		t.Fatalf(
			"fallback Send() calls = %d, want 0",
			fallback.sendCalls,
		)
	}
}

func TestSMSRouterProviderSuccessResetsCircuitFailureCount(
	t *testing.T,
) {
	primary := &testTrackedSMSProvider{
		err: &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorTemporary,
		},
	}

	fallback := &testTrackedSMSProvider{}

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

	router, err := NewSMSRouter(
		nil,
		"bulksmsiraq",
		primary,
		WithSMSFallbackProvider(
			"telnyx",
			fallback,
			ConservativeProviderFailoverPolicy{},
		),
		WithSMSProviderHealthTracker(
			healthTracker,
		),
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

	if err := router.Send(
		context.Background(),
		message,
	); err == nil {
		t.Fatal(
			"first Send() returned nil error",
		)
	}

	primary.err = nil

	if err := router.Send(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf(
			"successful Send() returned an error: %v",
			err,
		)
	}

	primary.err = &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorTemporary,
	}

	if err := router.Send(
		context.Background(),
		message,
	); err == nil {
		t.Fatal(
			"third Send() returned nil error",
		)
	}

	primary.err = nil

	if err := router.Send(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf(
			"fourth Send() returned an error: %v",
			err,
		)
	}

	if primary.sendCalls != 4 {
		t.Fatalf(
			"primary Send() calls = %d, want 4",
			primary.sendCalls,
		)
	}

	if fallback.sendCalls != 0 {
		t.Fatalf(
			"fallback Send() calls = %d, want 0",
			fallback.sendCalls,
		)
	}
}
