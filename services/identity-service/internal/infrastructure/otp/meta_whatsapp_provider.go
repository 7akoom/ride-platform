package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type MetaWhatsAppTemplate struct {
	Name         string
	LanguageCode string
}

type MetaWhatsAppProviderConfig struct {
	Endpoint    string
	AccessToken string
	Templates   map[string]MetaWhatsAppTemplate
}

type MetaWhatsAppProvider struct {
	httpClient  HTTPDoer
	endpoint    string
	accessToken string
	templates   map[string]MetaWhatsAppTemplate
}

type metaWhatsAppSendRequest struct {
	MessagingProduct string               `json:"messaging_product"`
	RecipientType    string               `json:"recipient_type"`
	To               string               `json:"to"`
	Type             string               `json:"type"`
	Template         metaWhatsAppTemplate `json:"template"`
}

type metaWhatsAppTemplate struct {
	Name       string                          `json:"name"`
	Language   metaWhatsAppTemplateLanguage    `json:"language"`
	Components []metaWhatsAppTemplateComponent `json:"components"`
}

type metaWhatsAppTemplateLanguage struct {
	Code string `json:"code"`
}

type metaWhatsAppTemplateComponent struct {
	Type       string                          `json:"type"`
	SubType    string                          `json:"sub_type,omitempty"`
	Index      string                          `json:"index,omitempty"`
	Parameters []metaWhatsAppTemplateParameter `json:"parameters"`
}

type metaWhatsAppTemplateParameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewMetaWhatsAppProvider(
	httpClient HTTPDoer,
	config MetaWhatsAppProviderConfig,
) (*MetaWhatsAppProvider, error) {
	if httpClient == nil {
		return nil, errors.New(
			"Meta WhatsApp HTTP client is required",
		)
	}

	endpoint := strings.TrimSpace(
		config.Endpoint,
	)
	if endpoint == "" {
		return nil, errors.New(
			"Meta WhatsApp endpoint is required",
		)
	}

	parsedEndpoint, err := url.ParseRequestURI(
		endpoint,
	)
	if err != nil ||
		parsedEndpoint.Scheme != "https" ||
		parsedEndpoint.Host != "graph.facebook.com" {
		return nil, errors.New(
			"Meta WhatsApp endpoint is invalid",
		)
	}

	accessToken := strings.TrimSpace(
		config.AccessToken,
	)
	if accessToken == "" {
		return nil, errors.New(
			"Meta WhatsApp access token is required",
		)
	}

	templates := make(
		map[string]MetaWhatsAppTemplate,
		len(config.Templates),
	)

	for rawLocale, rawTemplate := range config.Templates {
		locale := normalizeMetaWhatsAppLocale(
			rawLocale,
		)
		if locale == "" {
			return nil, errors.New(
				"Meta WhatsApp template locale is required",
			)
		}

		templateName := strings.TrimSpace(
			rawTemplate.Name,
		)
		if templateName == "" {
			return nil, fmt.Errorf(
				"Meta WhatsApp template name is required for locale %q",
				locale,
			)
		}

		languageCode := strings.TrimSpace(
			rawTemplate.LanguageCode,
		)
		if languageCode == "" {
			return nil, fmt.Errorf(
				"Meta WhatsApp template language code is required for locale %q",
				locale,
			)
		}

		if _, exists := templates[locale]; exists {
			return nil, fmt.Errorf(
				"duplicate Meta WhatsApp template locale %q",
				locale,
			)
		}

		templates[locale] = MetaWhatsAppTemplate{
			Name:         templateName,
			LanguageCode: languageCode,
		}
	}

	if _, exists := templates["en"]; !exists {
		return nil, errors.New(
			"Meta WhatsApp English template is required",
		)
	}

	return &MetaWhatsAppProvider{
		httpClient:  httpClient,
		endpoint:    endpoint,
		accessToken: accessToken,
		templates:   templates,
	}, nil
}

func (p *MetaWhatsAppProvider) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	phoneNumber := strings.TrimSpace(
		input.PhoneNumber,
	)
	if phoneNumber == "" {
		return errors.New(
			"Meta WhatsApp recipient is required",
		)
	}

	phoneNumber = strings.TrimPrefix(
		phoneNumber,
		"+",
	)

	for _, character := range phoneNumber {
		if character < '0' ||
			character > '9' {
			return errors.New(
				"Meta WhatsApp recipient must contain only digits",
			)
		}
	}

	code := strings.TrimSpace(
		input.Code,
	)
	if code == "" {
		return errors.New(
			"Meta WhatsApp OTP code is required",
		)
	}

	if _, err := auth.ParseOTPPurpose(
		string(input.Purpose),
	); err != nil {
		return fmt.Errorf(
			"validate Meta WhatsApp OTP purpose: %w",
			err,
		)
	}

	template, err := p.templateForLocale(
		input.Locale,
	)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(
		metaWhatsAppSendRequest{
			MessagingProduct: "whatsapp",
			RecipientType:    "individual",
			To:               phoneNumber,
			Type:             "template",
			Template: metaWhatsAppTemplate{
				Name: template.Name,
				Language: metaWhatsAppTemplateLanguage{
					Code: template.LanguageCode,
				},
				Components: []metaWhatsAppTemplateComponent{
					{
						Type: "body",
						Parameters: []metaWhatsAppTemplateParameter{
							{
								Type: "text",
								Text: code,
							},
						},
					},
					{
						Type:    "button",
						SubType: "url",
						Index:   "0",
						Parameters: []metaWhatsAppTemplateParameter{
							{
								Type: "text",
								Text: code,
							},
						},
					},
				},
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"encode Meta WhatsApp request: %w",
			err,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf(
			"create Meta WhatsApp request: %w",
			err,
		)
	}

	request.Header.Set(
		"Authorization",
		"Bearer "+p.accessToken,
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	request.Header.Set(
		"Accept",
		"application/json",
	)

	response, err := p.httpClient.Do(
		request,
	)
	if err != nil {
		return &WhatsAppProviderError{
			Provider: "meta",
			Kind:     WhatsAppProviderErrorUnknownDeliveryState,
			Err:      err,
		}
	}

	if response == nil {
		return &WhatsAppProviderError{
			Provider: "meta",
			Kind:     WhatsAppProviderErrorUnknownDeliveryState,
		}
	}

	if response.Body != nil {
		defer response.Body.Close()
	}

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		kind := WhatsAppProviderErrorPermanent

		switch {
		case response.StatusCode ==
			http.StatusTooManyRequests:
			kind = WhatsAppProviderErrorRateLimited

		case response.StatusCode >=
			http.StatusInternalServerError:
			kind = WhatsAppProviderErrorTemporary
		}

		return &WhatsAppProviderError{
			Provider:   "meta",
			Kind:       kind,
			StatusCode: response.StatusCode,
		}
	}

	return nil
}

func (p *MetaWhatsAppProvider) templateForLocale(
	value string,
) (MetaWhatsAppTemplate, error) {
	locale := normalizeMetaWhatsAppLocale(
		value,
	)

	if template, exists := p.templates[locale]; exists {
		return template, nil
	}

	template, exists := p.templates["en"]
	if !exists {
		return MetaWhatsAppTemplate{},
			errors.New(
				"Meta WhatsApp English fallback template is not configured",
			)
	}

	return template, nil
}

func normalizeMetaWhatsAppLocale(
	value string,
) string {
	value = strings.ToLower(
		strings.TrimSpace(value),
	)

	switch {
	case value == "ar" ||
		strings.HasPrefix(value, "ar-") ||
		strings.HasPrefix(value, "ar_"):
		return "ar"

	case value == "ku" ||
		strings.HasPrefix(value, "ku-") ||
		strings.HasPrefix(value, "ku_"):
		return "ku"

	case value == "en" ||
		strings.HasPrefix(value, "en-") ||
		strings.HasPrefix(value, "en_"):
		return "en"

	default:
		return "en"
	}
}
