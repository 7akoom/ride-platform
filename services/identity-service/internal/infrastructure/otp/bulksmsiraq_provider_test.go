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

type testHTTPDoer struct {
	calls    int
	request  *http.Request
	response *http.Response
	err      error
}

func (d *testHTTPDoer) Do(
	request *http.Request,
) (*http.Response, error) {
	d.calls++
	d.request = request

	return d.response, d.err
}

func TestNewBulkSMSIraqProviderAcceptsValidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewBulkSMSIraqProvider(
		httpClient,
		BulkSMSIraqProviderConfig{
			Endpoint: " https://sms.example.com/api/send ",
			APIKey:   " test-api-key ",
			SenderID: " Ride ",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewBulkSMSIraqProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"NewBulkSMSIraqProvider() returned nil provider",
		)
	}

	if provider.endpoint !=
		"https://sms.example.com/api/send" {
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

	if provider.senderID != "Ride" {
		t.Fatalf(
			"sender ID = %q, expected normalized sender ID",
			provider.senderID,
		)
	}
}

func TestNewBulkSMSIraqProviderRejectsInvalidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	tests := []struct {
		name       string
		httpClient HTTPDoer
		config     BulkSMSIraqProviderConfig
	}{
		{
			name:       "missing HTTP client",
			httpClient: nil,
			config: BulkSMSIraqProviderConfig{
				Endpoint: "https://sms.example.com/api/send",
				APIKey:   "test-api-key",
				SenderID: "Ride",
			},
		},
		{
			name:       "missing endpoint",
			httpClient: httpClient,
			config: BulkSMSIraqProviderConfig{
				APIKey:   "test-api-key",
				SenderID: "Ride",
			},
		},
		{
			name:       "invalid endpoint",
			httpClient: httpClient,
			config: BulkSMSIraqProviderConfig{
				Endpoint: "not-a-url",
				APIKey:   "test-api-key",
				SenderID: "Ride",
			},
		},
		{
			name:       "missing API key",
			httpClient: httpClient,
			config: BulkSMSIraqProviderConfig{
				Endpoint: "https://sms.example.com/api/send",
				SenderID: "Ride",
			},
		},
		{
			name:       "missing sender ID",
			httpClient: httpClient,
			config: BulkSMSIraqProviderConfig{
				Endpoint: "https://sms.example.com/api/send",
				APIKey:   "test-api-key",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := NewBulkSMSIraqProvider(
				testCase.httpClient,
				testCase.config,
			)

			if err == nil {
				t.Fatal(
					"NewBulkSMSIraqProvider() accepted invalid configuration",
				)
			}

			if provider != nil {
				t.Fatal(
					"NewBulkSMSIraqProvider() returned provider for invalid configuration",
				)
			}
		})
	}
}
func TestBulkSMSIraqProviderSendsSMSRequest(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(`{"success":true}`),
			),
		},
	}

	provider, err := NewBulkSMSIraqProvider(
		httpClient,
		BulkSMSIraqProviderConfig{
			Endpoint: "https://sms.example.com/api/send",
			APIKey:   "test-api-key",
			SenderID: "Ride",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewBulkSMSIraqProvider() returned an error: %v",
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
		"https://sms.example.com/api/send" {
		t.Fatalf(
			"request URL = %q, expected configured endpoint",
			request.URL.String(),
		)
	}

	if request.Header.Get("Authorization") !=
		"Bearer test-api-key" {
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

	var payload bulkSMSIraqSendRequest

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

	expectedPayload := bulkSMSIraqSendRequest{
		Recipient: "9647501234567",
		SenderID:  "Ride",
		Type:      "plain",
		Message:   "Your verification code is 123456",
	}

	if payload != expectedPayload {
		t.Fatalf(
			"request payload = %+v, expected %+v",
			payload,
			expectedPayload,
		)
	}
}
func TestBulkSMSIraqProviderRejectsInvalidRecipient(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewBulkSMSIraqProvider(
		httpClient,
		BulkSMSIraqProviderConfig{
			Endpoint: "https://sms.example.com/api/send",
			APIKey:   "test-api-key",
			SenderID: "Ride",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewBulkSMSIraqProvider() returned an error: %v",
			err,
		)
	}

	tests := []string{
		"",
		"   ",
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

func TestBulkSMSIraqProviderRejectsBlankMessage(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewBulkSMSIraqProvider(
		httpClient,
		BulkSMSIraqProviderConfig{
			Endpoint: "https://sms.example.com/api/send",
			APIKey:   "test-api-key",
			SenderID: "Ride",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewBulkSMSIraqProvider() returned an error: %v",
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

func TestBulkSMSIraqProviderPropagatesNetworkError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"connection refused",
	)

	httpClient := &testHTTPDoer{
		err: expectedErr,
	}

	provider, err := NewBulkSMSIraqProvider(
		httpClient,
		BulkSMSIraqProviderConfig{
			Endpoint: "https://sms.example.com/api/send",
			APIKey:   "test-api-key",
			SenderID: "Ride",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewBulkSMSIraqProvider() returned an error: %v",
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

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Send() error = %v, expected wrapped network error",
			err,
		)
	}
	var providerErr *SMSProviderError

	if !errors.As(err, &providerErr) {
		t.Fatalf(
			"Send() error = %T, expected *SMSProviderError",
			err,
		)
	}

	if providerErr.Provider != "bulksmsiraq" {
		t.Fatalf(
			"provider = %q, expected %q",
			providerErr.Provider,
			"bulksmsiraq",
		)
	}

	if providerErr.Kind != SMSProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			SMSProviderErrorUnknownDeliveryState,
		)
	}
}

func TestBulkSMSIraqProviderClassifiesHTTPFailures(
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

			provider, err := NewBulkSMSIraqProvider(
				httpClient,
				BulkSMSIraqProviderConfig{
					Endpoint: "https://sms.example.com/api/send",
					APIKey:   "test-api-key",
					SenderID: "Ride",
				},
			)
			if err != nil {
				t.Fatalf(
					"NewBulkSMSIraqProvider() returned an error: %v",
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

			if !errors.As(err, &providerErr) {
				t.Fatalf(
					"Send() error = %T, expected *SMSProviderError",
					err,
				)
			}

			if providerErr.Provider != "bulksmsiraq" {
				t.Fatalf(
					"provider = %q, expected %q",
					providerErr.Provider,
					"bulksmsiraq",
				)
			}

			if providerErr.Kind != testCase.expectedKind {
				t.Fatalf(
					"error kind = %q, expected %q",
					providerErr.Kind,
					testCase.expectedKind,
				)
			}

			if providerErr.StatusCode != testCase.statusCode {
				t.Fatalf(
					"status code = %d, expected %d",
					providerErr.StatusCode,
					testCase.statusCode,
				)
			}
		})
	}
}

func TestBulkSMSIraqProviderRejectsNilHTTPResponse(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: nil,
		err:      nil,
	}

	provider, err := NewBulkSMSIraqProvider(
		httpClient,
		BulkSMSIraqProviderConfig{
			Endpoint: "https://sms.example.com/api/send",
			APIKey:   "test-api-key",
			SenderID: "Ride",
		},
	)
	if err != nil {
		t.Fatalf(
			"NewBulkSMSIraqProvider() returned an error: %v",
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

	if !errors.As(err, &providerErr) {
		t.Fatalf(
			"Send() error = %T, expected *SMSProviderError",
			err,
		)
	}

	if providerErr.Kind != SMSProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			SMSProviderErrorUnknownDeliveryState,
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
}
