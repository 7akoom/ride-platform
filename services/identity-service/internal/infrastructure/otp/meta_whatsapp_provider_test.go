package otp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func validMetaWhatsAppProviderConfig() MetaWhatsAppProviderConfig {
	return MetaWhatsAppProviderConfig{
		Endpoint:    "https://graph.facebook.com/v23.0/123456789/messages",
		AccessToken: "test-meta-access-token",
		Templates: map[string]MetaWhatsAppTemplate{
			"en": {
				Name:         "ride_authentication",
				LanguageCode: "en_US",
			},
			"ar": {
				Name:         "ride_authentication",
				LanguageCode: "ar",
			},
		},
	}
}

func TestNewMetaWhatsAppProviderAcceptsValidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	config := validMetaWhatsAppProviderConfig()
	config.Endpoint =
		" https://graph.facebook.com/v23.0/123456789/messages "
	config.AccessToken = " test-meta-access-token "

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		config,
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	if provider == nil {
		t.Fatal(
			"NewMetaWhatsAppProvider() returned nil provider",
		)
	}

	if provider.endpoint !=
		"https://graph.facebook.com/v23.0/123456789/messages" {
		t.Fatalf(
			"endpoint = %q, expected normalized endpoint",
			provider.endpoint,
		)
	}

	if provider.accessToken !=
		"test-meta-access-token" {
		t.Fatal(
			"access token was not normalized",
		)
	}

	englishTemplate, exists :=
		provider.templates["en"]
	if !exists {
		t.Fatal(
			"English template was not configured",
		)
	}

	if englishTemplate.Name !=
		"ride_authentication" {
		t.Fatalf(
			"English template name = %q, expected %q",
			englishTemplate.Name,
			"ride_authentication",
		)
	}

	if englishTemplate.LanguageCode !=
		"en_US" {
		t.Fatalf(
			"English language code = %q, expected %q",
			englishTemplate.LanguageCode,
			"en_US",
		)
	}
}

func TestNewMetaWhatsAppProviderRejectsInvalidConfiguration(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	tests := []struct {
		name       string
		httpClient HTTPDoer
		config     MetaWhatsAppProviderConfig
	}{
		{
			name:       "missing HTTP client",
			httpClient: nil,
			config:     validMetaWhatsAppProviderConfig(),
		},
		{
			name:       "missing endpoint",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Endpoint = ""
				return cfg
			}(),
		},
		{
			name:       "invalid endpoint",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Endpoint = "not-a-url"
				return cfg
			}(),
		},
		{
			name:       "non HTTPS endpoint",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Endpoint =
					"http://graph.facebook.com/v23.0/123456789/messages"
				return cfg
			}(),
		},
		{
			name:       "non Meta endpoint",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Endpoint =
					"https://example.com/v23.0/123456789/messages"
				return cfg
			}(),
		},
		{
			name:       "missing access token",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.AccessToken = " "
				return cfg
			}(),
		},
		{
			name:       "missing templates",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Templates = nil
				return cfg
			}(),
		},
		{
			name:       "missing English template",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Templates =
					map[string]MetaWhatsAppTemplate{
						"ar": {
							Name:         "ride_authentication",
							LanguageCode: "ar",
						},
					}
				return cfg
			}(),
		},
		{
			name:       "blank template name",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Templates["en"] =
					MetaWhatsAppTemplate{
						Name:         " ",
						LanguageCode: "en_US",
					}
				return cfg
			}(),
		},
		{
			name:       "blank template language code",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Templates["en"] =
					MetaWhatsAppTemplate{
						Name:         "ride_authentication",
						LanguageCode: " ",
					}
				return cfg
			}(),
		},
		{
			name:       "duplicate normalized locale",
			httpClient: httpClient,
			config: func() MetaWhatsAppProviderConfig {
				cfg := validMetaWhatsAppProviderConfig()
				cfg.Templates =
					map[string]MetaWhatsAppTemplate{
						"en": {
							Name:         "ride_authentication",
							LanguageCode: "en_US",
						},
						"EN": {
							Name:         "ride_authentication_alt",
							LanguageCode: "en_GB",
						},
					}
				return cfg
			}(),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err :=
				NewMetaWhatsAppProvider(
					testCase.httpClient,
					testCase.config,
				)

			if err == nil {
				t.Fatal(
					"NewMetaWhatsAppProvider() accepted invalid configuration",
				)
			}

			if provider != nil {
				t.Fatal(
					"NewMetaWhatsAppProvider() returned provider for invalid configuration",
				)
			}
		})
	}
}

func TestMetaWhatsAppProviderSendsAuthenticationTemplate(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(
					`{"messages":[{"id":"wamid.test"}]}`,
				),
			),
		},
	}

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: " +9647501234567 ",
			Code:        " 123456 ",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      " ar ",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
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
		"https://graph.facebook.com/v23.0/123456789/messages" {
		t.Fatalf(
			"request URL = %q, expected configured endpoint",
			request.URL.String(),
		)
	}

	if request.Header.Get("Authorization") !=
		"Bearer test-meta-access-token" {
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

	var payload metaWhatsAppSendRequest

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

	if payload.MessagingProduct != "whatsapp" {
		t.Fatalf(
			"messaging product = %q, expected %q",
			payload.MessagingProduct,
			"whatsapp",
		)
	}

	if payload.RecipientType != "individual" {
		t.Fatalf(
			"recipient type = %q, expected %q",
			payload.RecipientType,
			"individual",
		)
	}

	if payload.To != "9647501234567" {
		t.Fatalf(
			"recipient = %q, expected %q",
			payload.To,
			"9647501234567",
		)
	}

	if payload.Type != "template" {
		t.Fatalf(
			"message type = %q, expected %q",
			payload.Type,
			"template",
		)
	}

	if payload.Template.Name !=
		"ride_authentication" {
		t.Fatalf(
			"template name = %q, expected %q",
			payload.Template.Name,
			"ride_authentication",
		)
	}

	if payload.Template.Language.Code != "ar" {
		t.Fatalf(
			"template language = %q, expected %q",
			payload.Template.Language.Code,
			"ar",
		)
	}

	if len(payload.Template.Components) != 2 {
		t.Fatalf(
			"component count = %d, expected 2",
			len(payload.Template.Components),
		)
	}

	bodyComponent :=
		payload.Template.Components[0]

	if bodyComponent.Type != "body" {
		t.Fatalf(
			"body component type = %q, expected %q",
			bodyComponent.Type,
			"body",
		)
	}

	if len(bodyComponent.Parameters) != 1 {
		t.Fatalf(
			"body parameter count = %d, expected 1",
			len(bodyComponent.Parameters),
		)
	}

	if bodyComponent.Parameters[0].Type != "text" ||
		bodyComponent.Parameters[0].Text != "123456" {
		t.Fatalf(
			"unexpected body OTP parameter: %+v",
			bodyComponent.Parameters[0],
		)
	}

	buttonComponent :=
		payload.Template.Components[1]

	if buttonComponent.Type != "button" {
		t.Fatalf(
			"button component type = %q, expected %q",
			buttonComponent.Type,
			"button",
		)
	}

	if buttonComponent.SubType != "url" {
		t.Fatalf(
			"button subtype = %q, expected %q",
			buttonComponent.SubType,
			"url",
		)
	}

	if buttonComponent.Index != "0" {
		t.Fatalf(
			"button index = %q, expected %q",
			buttonComponent.Index,
			"0",
		)
	}

	if len(buttonComponent.Parameters) != 1 {
		t.Fatalf(
			"button parameter count = %d, expected 1",
			len(buttonComponent.Parameters),
		)
	}

	if buttonComponent.Parameters[0].Type != "text" ||
		buttonComponent.Parameters[0].Text != "123456" {
		t.Fatalf(
			"unexpected button OTP parameter: %+v",
			buttonComponent.Parameters[0],
		)
	}
}

func TestMetaWhatsAppProviderUsesConfiguredLocaleTemplate(
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

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "ar-IQ",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	var payload metaWhatsAppSendRequest

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

	if payload.Template.Language.Code != "ar" {
		t.Fatalf(
			"template language = %q, expected %q",
			payload.Template.Language.Code,
			"ar",
		)
	}
}

func TestMetaWhatsAppProviderFallsBackToEnglishTemplate(
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

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "ku",
		},
	)
	if err != nil {
		t.Fatalf(
			"SendOTP() returned an error: %v",
			err,
		)
	}

	var payload metaWhatsAppSendRequest

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

	if payload.Template.Name !=
		"ride_authentication" {
		t.Fatalf(
			"template name = %q, expected English fallback template",
			payload.Template.Name,
		)
	}

	if payload.Template.Language.Code !=
		"en_US" {
		t.Fatalf(
			"template language = %q, expected %q",
			payload.Template.Language.Code,
			"en_US",
		)
	}
}

func TestMetaWhatsAppProviderRejectsInvalidRecipient(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
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
			err := provider.SendOTP(
				context.Background(),
				WhatsAppOTPProviderInput{
					PhoneNumber: recipient,
					Code:        "123456",
					Purpose:     auth.OTPPurposeLogin,
					Locale:      "en",
				},
			)

			if err == nil {
				t.Fatal(
					"SendOTP() accepted invalid recipient",
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

func TestMetaWhatsAppProviderRejectsBlankOTPCode(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        " ",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "en",
		},
	)

	if err == nil {
		t.Fatal(
			"SendOTP() accepted blank OTP code",
		)
	}

	if httpClient.calls != 0 {
		t.Fatalf(
			"HTTP calls = %d, expected 0",
			httpClient.calls,
		)
	}
}

func TestMetaWhatsAppProviderRejectsInvalidPurpose(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{}

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurpose("invalid"),
			Locale:      "en",
		},
	)

	if !errors.Is(
		err,
		auth.ErrInvalidOTPPurpose,
	) {
		t.Fatalf(
			"SendOTP() error = %v, expected %v",
			err,
			auth.ErrInvalidOTPPurpose,
		)
	}

	if httpClient.calls != 0 {
		t.Fatalf(
			"HTTP calls = %d, expected 0",
			httpClient.calls,
		)
	}
}

func TestMetaWhatsAppProviderPropagatesNetworkError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"connection refused",
	)

	httpClient := &testHTTPDoer{
		err: expectedErr,
	}

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "en",
		},
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"SendOTP() error = %v, expected wrapped network error",
			err,
		)
	}

	var providerErr *WhatsAppProviderError

	if !errors.As(err, &providerErr) {
		t.Fatalf(
			"SendOTP() error = %T, expected *WhatsAppProviderError",
			err,
		)
	}

	if providerErr.Provider != "meta" {
		t.Fatalf(
			"provider = %q, expected %q",
			providerErr.Provider,
			"meta",
		)
	}

	if providerErr.Kind !=
		WhatsAppProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			WhatsAppProviderErrorUnknownDeliveryState,
		)
	}
}

func TestMetaWhatsAppProviderClassifiesHTTPFailures(
	t *testing.T,
) {
	tests := []struct {
		name         string
		statusCode   int
		expectedKind WhatsAppProviderErrorKind
	}{
		{
			name:         "rate limited",
			statusCode:   http.StatusTooManyRequests,
			expectedKind: WhatsAppProviderErrorRateLimited,
		},
		{
			name:         "temporary server failure",
			statusCode:   http.StatusServiceUnavailable,
			expectedKind: WhatsAppProviderErrorTemporary,
		},
		{
			name:         "permanent client failure",
			statusCode:   http.StatusBadRequest,
			expectedKind: WhatsAppProviderErrorPermanent,
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

			provider, err :=
				NewMetaWhatsAppProvider(
					httpClient,
					validMetaWhatsAppProviderConfig(),
				)
			if err != nil {
				t.Fatalf(
					"NewMetaWhatsAppProvider() returned an error: %v",
					err,
				)
			}

			err = provider.SendOTP(
				context.Background(),
				WhatsAppOTPProviderInput{
					PhoneNumber: "+9647501234567",
					Code:        "123456",
					Purpose:     auth.OTPPurposeLogin,
					Locale:      "en",
				},
			)

			if err == nil {
				t.Fatal(
					"SendOTP() accepted failed HTTP response",
				)
			}

			var providerErr *WhatsAppProviderError

			if !errors.As(
				err,
				&providerErr,
			) {
				t.Fatalf(
					"SendOTP() error = %T, expected *WhatsAppProviderError",
					err,
				)
			}

			if providerErr.Provider != "meta" {
				t.Fatalf(
					"provider = %q, expected %q",
					providerErr.Provider,
					"meta",
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

func TestMetaWhatsAppProviderRejectsNilHTTPResponse(
	t *testing.T,
) {
	httpClient := &testHTTPDoer{
		response: nil,
		err:      nil,
	}

	provider, err := NewMetaWhatsAppProvider(
		httpClient,
		validMetaWhatsAppProviderConfig(),
	)
	if err != nil {
		t.Fatalf(
			"NewMetaWhatsAppProvider() returned an error: %v",
			err,
		)
	}

	err = provider.SendOTP(
		context.Background(),
		WhatsAppOTPProviderInput{
			PhoneNumber: "+9647501234567",
			Code:        "123456",
			Purpose:     auth.OTPPurposeLogin,
			Locale:      "en",
		},
	)

	if err == nil {
		t.Fatal(
			"SendOTP() accepted nil HTTP response",
		)
	}

	var providerErr *WhatsAppProviderError

	if !errors.As(
		err,
		&providerErr,
	) {
		t.Fatalf(
			"SendOTP() error = %T, expected *WhatsAppProviderError",
			err,
		)
	}

	if providerErr.Kind !=
		WhatsAppProviderErrorUnknownDeliveryState {
		t.Fatalf(
			"error kind = %q, expected %q",
			providerErr.Kind,
			WhatsAppProviderErrorUnknownDeliveryState,
		)
	}
}
