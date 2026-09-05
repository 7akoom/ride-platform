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

func TestNewResendProviderAcceptsValidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewResendProvider(
		httpClient,
		ResendProviderConfig{
			Endpoint: " https://api.resend.com/emails ",
			APIKey:   " test-api-key ",
			From:     " Ride <no-reply@example.com> ",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewResendProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"NewResendProvider() returned nil provider",
		)
	}

	if provider.endpoint !=
		"https://api.resend.com/emails" {
		t.Fatalf(
			"endpoint = %q, expected normalized endpoint",
			provider.endpoint,
		)
	}

	if provider.apiKey != "test-api-key" {
		t.Fatalf(
			"API key = %q, expected normalized API key",
			provider.apiKey,
		)
	}

	if provider.from !=
		"Ride <no-reply@example.com>" {
		t.Fatalf(
			"sender = %q, expected normalized sender",
			provider.from,
		)
	}
}

func TestNewResendProviderRejectsInvalidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	tests := []struct {
		name       string
		httpClient HTTPDoer
		config     ResendProviderConfig
	}{
		{
			name:       "missing HTTP client",
			httpClient: nil,
			config: ResendProviderConfig{
				Endpoint: "https://api.resend.com/emails",
				APIKey:   "test-api-key",
				From:     "Ride <no-reply@example.com>",
			},
		},
		{
			name:       "missing endpoint",
			httpClient: httpClient,
			config: ResendProviderConfig{
				APIKey: "test-api-key",
				From:   "Ride <no-reply@example.com>",
			},
		},
		{
			name:       "invalid endpoint",
			httpClient: httpClient,
			config: ResendProviderConfig{
				Endpoint: "not-a-url",
				APIKey:   "test-api-key",
				From:     "Ride <no-reply@example.com>",
			},
		},
		{
			name:       "missing API key",
			httpClient: httpClient,
			config: ResendProviderConfig{
				Endpoint: "https://api.resend.com/emails",
				From:     "Ride <no-reply@example.com>",
			},
		},
		{
			name:       "missing sender",
			httpClient: httpClient,
			config: ResendProviderConfig{
				Endpoint: "https://api.resend.com/emails",
				APIKey:   "test-api-key",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := NewResendProvider(
				testCase.httpClient,
				testCase.config,
			)

			if err == nil {
				t.Fatal(
					"NewResendProvider() accepted invalid configuration",
				)
			}

			if provider != nil {
				t.Fatal(
					"NewResendProvider() returned provider for invalid configuration",
				)
			}
		})
	}
}

func TestResendProviderSendsEmailRequest(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(
					`{"id":"email-123"}`,
				),
			),
		},
	}

	provider, err := NewResendProvider(
		httpClient,
		ResendProviderConfig{
			Endpoint: "https://api.resend.com/emails",
			APIKey:   "test-api-key",
			From:     "Ride <no-reply@example.com>",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewResendProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		EmailMessage{
			To:       "  user@example.com  ",
			Subject:  " Ride verification code ",
			TextBody: " Your code is 123456. ",
			HTMLBody: " <p>Your code is 123456.</p> ",
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
		"https://api.resend.com/emails" {
		t.Fatalf(
			"request URL = %q, expected configured endpoint",
			request.URL.String(),
		)
	}

	if request.Header.Get(
		"Authorization",
	) != "Bearer test-api-key" {
		t.Fatal(
			"unexpected Authorization header",
		)
	}

	if request.Header.Get(
		"Content-Type",
	) != "application/json" {
		t.Fatal(
			"unexpected Content-Type header",
		)
	}

	if request.Header.Get(
		"Accept",
	) != "application/json" {
		t.Fatal(
			"unexpected Accept header",
		)
	}

	var payload resendSendEmailRequest

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

	expectedPayload := resendSendEmailRequest{
		From: "Ride <no-reply@example.com>",
		To: []string{
			"user@example.com",
		},
		Subject: "Ride verification code",
		Text:    "Your code is 123456.",
		HTML:    "<p>Your code is 123456.</p>",
	}

	if payload.From != expectedPayload.From {
		t.Fatalf(
			"from = %q, expected %q",
			payload.From,
			expectedPayload.From,
		)
	}

	if len(payload.To) != 1 ||
		payload.To[0] !=
			expectedPayload.To[0] {
		t.Fatalf(
			"to = %+v, expected %+v",
			payload.To,
			expectedPayload.To,
		)
	}

	if payload.Subject !=
		expectedPayload.Subject {
		t.Fatalf(
			"subject = %q, expected %q",
			payload.Subject,
			expectedPayload.Subject,
		)
	}

	if payload.Text != expectedPayload.Text {
		t.Fatalf(
			"text = %q, expected %q",
			payload.Text,
			expectedPayload.Text,
		)
	}

	if payload.HTML != expectedPayload.HTML {
		t.Fatalf(
			"HTML = %q, expected %q",
			payload.HTML,
			expectedPayload.HTML,
		)
	}
}

func TestResendProviderRejectsInvalidMessage(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewResendProvider(
		httpClient,
		ResendProviderConfig{
			Endpoint: "https://api.resend.com/emails",
			APIKey:   "test-api-key",
			From:     "Ride <no-reply@example.com>",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewResendProvider() returned an error: %v",
			err,
		)
	}

	tests := []struct {
		name    string
		message EmailMessage
	}{
		{
			name: "missing recipient",
			message: EmailMessage{
				Subject:  "Verification code",
				TextBody: "Your code is 123456.",
			},
		},
		{
			name: "missing subject",
			message: EmailMessage{
				To:       "user@example.com",
				TextBody: "Your code is 123456.",
			},
		},
		{
			name: "missing body",
			message: EmailMessage{
				To:      "user@example.com",
				Subject: "Verification code",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := provider.Send(
				context.Background(),
				testCase.message,
			)

			if err == nil {
				t.Fatal(
					"Send() accepted invalid email message",
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

func TestResendProviderClassifiesTransportFailureAsUnknownDeliveryState(
	t *testing.T,
) {
	expectedErr := errors.New(
		"network connection lost",
	)

	httpClient := &testHTTPDoer{
		err: expectedErr,
	}

	provider, err := NewResendProvider(
		httpClient,
		ResendProviderConfig{
			Endpoint: "https://api.resend.com/emails",
			APIKey:   "test-api-key",
			From:     "Ride <no-reply@example.com>",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewResendProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		EmailMessage{
			To:       "user@example.com",
			Subject:  "Verification code",
			TextBody: "Your code is 123456.",
		},
	)

	var providerErr *EmailProviderError
	if !errors.As(
		err,
		&providerErr,
	) {
		t.Fatalf(
			"Send() error = %v, expected EmailProviderError",
			err,
		)
	}

	if providerErr.Provider != "resend" {
		t.Fatalf(
			"provider = %q, expected %q",
			providerErr.Provider,
			"resend",
		)
	}

	if providerErr.Kind !=
		EmailProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			EmailProviderErrorUnknownDeliveryState,
		)
	}

	if !errors.Is(
		err,
		expectedErr,
	) {
		t.Fatalf(
			"Send() error does not wrap transport error",
		)
	}
}

func TestResendProviderClassifiesHTTPFailures(
	t *testing.T,
) {
	tests := []struct {
		name       string
		statusCode int
		expected   EmailProviderErrorKind
	}{
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			expected:   EmailProviderErrorRateLimited,
		},
		{
			name:       "server error",
			statusCode: http.StatusServiceUnavailable,
			expected:   EmailProviderErrorTemporary,
		},
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			expected:   EmailProviderErrorPermanent,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			expected:   EmailProviderErrorPermanent,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			httpClient := &testHTTPDoer{
				response: &http.Response{
					StatusCode: testCase.statusCode,
					Body: io.NopCloser(
						strings.NewReader(
							`{"message":"provider error"}`,
						),
					),
				},
			}

			provider, err := NewResendProvider(
				httpClient,
				ResendProviderConfig{
					Endpoint: "https://api.resend.com/emails",
					APIKey:   "test-api-key",
					From:     "Ride <no-reply@example.com>",
				},
			)
			if err != nil {
				t.Fatalf(
					"NewResendProvider() returned an error: %v",
					err,
				)
			}

			err = provider.Send(
				context.Background(),
				EmailMessage{
					To:       "user@example.com",
					Subject:  "Verification code",
					TextBody: "Your code is 123456.",
				},
			)

			var providerErr *EmailProviderError
			if !errors.As(
				err,
				&providerErr,
			) {
				t.Fatalf(
					"Send() error = %v, expected EmailProviderError",
					err,
				)
			}

			if providerErr.Kind !=
				testCase.expected {
				t.Fatalf(
					"error kind = %q, expected %q",
					providerErr.Kind,
					testCase.expected,
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

func TestResendProviderTreatsNilHTTPResponseAsUnknownDeliveryState(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewResendProvider(
		httpClient,
		ResendProviderConfig{
			Endpoint: "https://api.resend.com/emails",
			APIKey:   "test-api-key",
			From:     "Ride <no-reply@example.com>",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewResendProvider() returned an error: %v",
			err,
		)
	}

	err = provider.Send(
		context.Background(),
		EmailMessage{
			To:       "user@example.com",
			Subject:  "Verification code",
			TextBody: "Your code is 123456.",
		},
	)

	var providerErr *EmailProviderError
	if !errors.As(
		err,
		&providerErr,
	) {
		t.Fatalf(
			"Send() error = %v, expected EmailProviderError",
			err,
		)
	}

	if providerErr.Kind !=
		EmailProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			EmailProviderErrorUnknownDeliveryState,
		)
	}
}
