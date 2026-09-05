package otp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func validTelnyxProviderConfig() TelnyxProviderConfig {
	return TelnyxProviderConfig{
		Endpoint:           "https://api.telnyx.com/v2/messages",
		APIKey:             "test-telnyx-api-key",
		From:               "Ride",
		MessagingProfileID: "profile-123",
	}
}

func TestNewTelnyxProviderAcceptsValidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewTelnyxProvider(
		httpClient,
		TelnyxProviderConfig{
			Endpoint:           " https://api.telnyx.com/v2/messages ",
			APIKey:             " test-telnyx-api-key ",
			From:               " Ride ",
			MessagingProfileID: " profile-123 ",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"NewTelnyxProvider() returned nil provider",
		)
	}

	if provider.endpoint !=
		"https://api.telnyx.com/v2/messages" {
		t.Fatalf(
			"endpoint = %q, expected normalized endpoint",
			provider.endpoint,
		)
	}

	if provider.apiKey !=
		"test-telnyx-api-key" {
		t.Fatal(
			"API key was not normalized",
		)
	}

	if provider.from != "Ride" {
		t.Fatalf(
			"sender = %q, expected %q",
			provider.from,
			"Ride",
		)
	}

	if provider.messagingProfileID !=
		"profile-123" {
		t.Fatalf(
			"messaging profile ID = %q, expected %q",
			provider.messagingProfileID,
			"profile-123",
		)
	}
}

func TestNewTelnyxProviderAllowsNumericSenderWithoutMessagingProfile(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewTelnyxProvider(
		httpClient,
		TelnyxProviderConfig{
			Endpoint: "https://api.telnyx.com/v2/messages",
			APIKey:   "test-telnyx-api-key",
			From:     "+12025550123",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"NewTelnyxProvider() returned nil provider",
		)
	}

	if provider.messagingProfileID != "" {
		t.Fatalf(
			"messaging profile ID = %q, expected empty value",
			provider.messagingProfileID,
		)
	}
}

func TestNewTelnyxProviderRejectsInvalidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	tests := []struct {
		name       string
		httpClient HTTPDoer
		config     TelnyxProviderConfig
	}{
		{
			name:       "missing HTTP client",
			httpClient: nil,
			config:     validTelnyxProviderConfig(),
		},
		{
			name:       "missing endpoint",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.Endpoint = ""
				return cfg
			}(),
		},
		{
			name:       "invalid endpoint",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.Endpoint = "not-a-url"
				return cfg
			}(),
		},
		{
			name:       "non HTTPS endpoint",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.Endpoint =
					"http://api.telnyx.com/v2/messages"
				return cfg
			}(),
		},
		{
			name:       "non Telnyx endpoint",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.Endpoint =
					"https://example.com/v2/messages"
				return cfg
			}(),
		},
		{
			name:       "missing API key",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.APIKey = " "
				return cfg
			}(),
		},
		{
			name:       "missing sender",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.From = " "
				return cfg
			}(),
		},
		{
			name:       "alphanumeric sender without messaging profile",
			httpClient: httpClient,
			config: func() TelnyxProviderConfig {
				cfg := validTelnyxProviderConfig()
				cfg.From = "Ride"
				cfg.MessagingProfileID = ""
				return cfg
			}(),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := NewTelnyxProvider(
				testCase.httpClient,
				testCase.config,
			)

			if err == nil {
				t.Fatal(
					"NewTelnyxProvider() accepted invalid configuration",
				)
			}

			if provider != nil {
				t.Fatal(
					"NewTelnyxProvider() returned provider for invalid configuration",
				)
			}
		})
	}
}

func TestTelnyxProviderSendsSMSRequest(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(
					`{"data":{"id":"message-123"}}`,
				),
			),
		},
	}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		SMSMessage{
			To:   " +9647501234567 ",
			Body: " Your verification code is 123456 ",
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if httpClient.calls != 1 {
		t.Fatalf(
			"HTTP calls = %d, expected 1",
			httpClient.calls,
		)
	}

	request := httpClient.request

	if request.Method != http.MethodPost {
		t.Fatalf(
			"HTTP method = %q, expected POST",
			request.Method,
		)
	}

	if request.URL.String() !=
		"https://api.telnyx.com/v2/messages" {
		t.Fatalf(
			"request URL = %q, expected configured endpoint",
			request.URL.String(),
		)
	}

	if request.Header.Get("Authorization") !=
		"Bearer test-telnyx-api-key" {
		t.Fatal(
			"unexpected Authorization header",
		)
	}

	if request.Header.Get("Content-Type") !=
		"application/json" {
		t.Fatal(
			"unexpected Content-Type header",
		)
	}

	if request.Header.Get("Accept") !=
		"application/json" {
		t.Fatal(
			"unexpected Accept header",
		)
	}

	var payload telnyxSendMessageRequest

	err = json.NewDecoder(
		request.Body,
	).Decode(
		&payload,
	)
	if err != nil {
		t.Fatalf(
			"decode request body: %v",
			err,
		)
	}

	expectedPayload := telnyxSendMessageRequest{
		From:               "Ride",
		To:                 "+9647501234567",
		Text:               "Your verification code is 123456",
		Type:               "SMS",
		MessagingProfileID: "profile-123",
	}

	if payload != expectedPayload {
		t.Fatalf(
			"request payload = %+v, expected %+v",
			payload,
			expectedPayload,
		)
	}
}

func TestTelnyxProviderOmitsOptionalMessagingProfile(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(`{}`),
			),
		},
	}

	provider, err := NewTelnyxProvider(
		httpClient,
		TelnyxProviderConfig{
			Endpoint: "https://api.telnyx.com/v2/messages",
			APIKey:   "test-telnyx-api-key",
			From:     "+12025550123",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	var payload map[string]any

	err = json.NewDecoder(
		httpClient.request.Body,
	).Decode(
		&payload,
	)
	if err != nil {
		t.Fatalf(
			"decode request body: %v",
			err,
		)
	}

	if _, exists :=
		payload["messaging_profile_id"]; exists {
		t.Fatal(
			"request unexpectedly included messaging_profile_id",
		)
	}
}

func TestTelnyxProviderRejectsInvalidRecipient(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	tests := []string{
		"",
		"   ",
		"9647501234567",
		"+",
		"+96475abc123",
		"+964 7501234567",
	}

	for _, recipient := range tests {
		t.Run(recipient, func(t *testing.T) {
			err := provider.Send(
				context.Background(),
				SMSMessage{
					To:   recipient,
					Body: "Your verification code is 123456",
				},
			)

			if err == nil {
				t.Fatal(
					"Send() accepted invalid recipient",
				)
			}
		})
	}

	if httpClient.calls != 0 {
		t.Fatalf(
			"HTTP calls = %d, expected 0",
			httpClient.calls,
		)
	}
}

func TestTelnyxProviderRejectsBlankMessage(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "   ",
		},
	)

	if err == nil {
		t.Fatal(
			"Send() accepted blank message body",
		)
	}

	if httpClient.calls != 0 {
		t.Fatalf(
			"HTTP calls = %d, expected 0",
			httpClient.calls,
		)
	}
}

func TestTelnyxProviderPropagatesNetworkError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"connection refused",
	)

	httpClient := &testHTTPDoer{
		err: expectedErr,
	}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	)

	if !errors.Is(
		err,
		expectedErr,
	) {
		t.Fatalf(
			"Send() error = %v, expected wrapped network error",
			err,
		)
	}

	var providerErr *SMSProviderError

	if !errors.As(
		err,
		&providerErr,
	) {
		t.Fatalf(
			"Send() error = %T, expected *SMSProviderError",
			err,
		)
	}

	if providerErr.Provider != "telnyx" {
		t.Fatalf(
			"provider = %q, expected %q",
			providerErr.Provider,
			"telnyx",
		)
	}

	if providerErr.Kind !=
		SMSProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			SMSProviderErrorUnknownDeliveryState,
		)
	}
}

func TestTelnyxProviderClassifiesHTTPFailures(
	t *testing.T,
) {
	tests := []struct {
		name         string
		statusCode   int
		expectedKind SMSProviderErrorKind
	}{
		{
			name:         "rate limited",
			statusCode:   http.StatusTooManyRequests,
			expectedKind: SMSProviderErrorRateLimited,
		},
		{
			name:         "temporary server failure",
			statusCode:   http.StatusServiceUnavailable,
			expectedKind: SMSProviderErrorTemporary,
		},
		{
			name:         "permanent client failure",
			statusCode:   http.StatusBadRequest,
			expectedKind: SMSProviderErrorPermanent,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			httpClient := &testHTTPDoer{
				response: &http.Response{
					StatusCode: testCase.statusCode,
					Body: io.NopCloser(
						strings.NewReader(`{}`),
					),
				},
			}

			provider, err := NewTelnyxProvider(
				httpClient,
				validTelnyxProviderConfig(),
			)
			if err != nil {
				t.Fatalf(
					"NewTelnyxProvider() returned an error: %v",
					err,
				)
			}

			err = provider.Send(
				context.Background(),
				SMSMessage{
					To:   "+9647501234567",
					Body: "Your verification code is 123456",
				},
			)

			if err == nil {
				t.Fatal(
					"Send() accepted failed HTTP response",
				)
			}

			var providerErr *SMSProviderError

			if !errors.As(
				err,
				&providerErr,
			) {
				t.Fatalf(
					"Send() error = %T, expected *SMSProviderError",
					err,
				)
			}

			if providerErr.Provider != "telnyx" {
				t.Fatalf(
					"provider = %q, expected %q",
					providerErr.Provider,
					"telnyx",
				)
			}

			if providerErr.Kind !=
				testCase.expectedKind {
				t.Fatalf(
					"error kind = %q, expected %q",
					providerErr.Kind,
					testCase.expectedKind,
				)
			}

			if providerErr.StatusCode !=
				testCase.statusCode {
				t.Fatalf(
					"status code = %d, expected %d",
					providerErr.StatusCode,
					testCase.statusCode,
				)
			}
		})
	}
}

func TestTelnyxProviderRejectsNilHTTPResponse(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: nil,
		err:      nil,
	}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	)

	if err == nil {
		t.Fatal(
			"Send() accepted nil HTTP response",
		)
	}

	var providerErr *SMSProviderError

	if !errors.As(
		err,
		&providerErr,
	) {
		t.Fatalf(
			"Send() error = %T, expected *SMSProviderError",
			err,
		)
	}

	if providerErr.Provider != "telnyx" {
		t.Fatalf(
			"provider = %q, expected %q",
			providerErr.Provider,
			"telnyx",
		)
	}

	if providerErr.Kind !=
		SMSProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			SMSProviderErrorUnknownDeliveryState,
		)
	}
}
func TestTelnyxProviderSendTrackedReturnsDeliveryResult(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(
					`{
						"data": {
							"id": "message-123",
							"to": [
								{
									"status": "queued"
								}
							]
						}
					}`,
				),
			),
		},
	}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	result, err := provider.SendTracked(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendTracked() returned an error: %v",
			err,
		)
	}

	if result.ProviderMessageID != "message-123" {
		t.Fatalf(
			"provider message ID = %q, want %q",
			result.ProviderMessageID,
			"message-123",
		)
	}

	if result.ProviderStatus != "queued" {
		t.Fatalf(
			"provider status = %q, want %q",
			result.ProviderStatus,
			"queued",
		)
	}
}

func TestTelnyxProviderSendTrackedRejectsMissingMessageID(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(
					`{
						"data": {
							"to": [
								{
									"status": "queued"
								}
							]
						}
					}`,
				),
			),
		},
	}

	provider, err := NewTelnyxProvider(
		httpClient,
		validTelnyxProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewTelnyxProvider() returned an error: %v",
			err,
		)
	}

	_, err = provider.SendTracked(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
	)
	if err == nil {
		t.Fatal(
			"SendTracked() returned nil error for missing message ID",
		)
	}

	var providerError *SMSProviderError

	if !errors.As(
		err,
		&providerError,
	) {
		t.Fatalf(
			"SendTracked() error = %T, want *SMSProviderError",
			err,
		)
	}

	if providerError.Provider != "telnyx" {
		t.Fatalf(
			"provider = %q, want %q",
			providerError.Provider,
			"telnyx",
		)
	}

	if providerError.Kind !=
		SMSProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, want %q",
			providerError.Kind,
			SMSProviderErrorUnknownDeliveryState,
		)
	}
}
