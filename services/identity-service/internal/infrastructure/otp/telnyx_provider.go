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
)

type TelnyxProviderConfig struct {
	Endpoint           string
	APIKey             string
	From               string
	MessagingProfileID string
}

type TelnyxProvider struct {
	httpClient         HTTPDoer
	endpoint           string
	apiKey             string
	from               string
	messagingProfileID string
}

type telnyxSendMessageRequest struct {
	From               string `json:"from"`
	To                 string `json:"to"`
	Text               string `json:"text"`
	Type               string `json:"type"`
	MessagingProfileID string `json:"messaging_profile_id,omitempty"`
}

func NewTelnyxProvider(
	httpClient HTTPDoer,
	config TelnyxProviderConfig,
) (*TelnyxProvider, error) {
	if httpClient == nil {
		return nil, errors.New(
			"Telnyx HTTP client is required",
		)
	}

	endpoint := strings.TrimSpace(
		config.Endpoint,
	)
	if endpoint == "" {
		return nil, errors.New(
			"Telnyx endpoint is required",
		)
	}

	parsedEndpoint, err := url.ParseRequestURI(
		endpoint,
	)
	if err != nil ||
		parsedEndpoint.Scheme != "https" ||
		parsedEndpoint.Host != "api.telnyx.com" {
		return nil, errors.New(
			"Telnyx endpoint is invalid",
		)
	}

	apiKey := strings.TrimSpace(
		config.APIKey,
	)
	if apiKey == "" {
		return nil, errors.New(
			"Telnyx API key is required",
		)
	}

	from := strings.TrimSpace(
		config.From,
	)
	if from == "" {
		return nil, errors.New(
			"Telnyx sender is required",
		)
	}

	messagingProfileID := strings.TrimSpace(
		config.MessagingProfileID,
	)

	if containsLetter(from) &&
		messagingProfileID == "" {
		return nil, errors.New(
			"Telnyx messaging profile ID is required for alphanumeric sender",
		)
	}

	return &TelnyxProvider{
		httpClient:         httpClient,
		endpoint:           endpoint,
		apiKey:             apiKey,
		from:               from,
		messagingProfileID: messagingProfileID,
	}, nil
}

func (p *TelnyxProvider) Send(
	ctx context.Context,
	message SMSMessage,
) error {
	recipient := strings.TrimSpace(
		message.To,
	)
	if recipient == "" {
		return errors.New(
			"Telnyx recipient is required",
		)
	}

	if recipient[0] != '+' {
		return errors.New(
			"Telnyx recipient must use international E.164 format",
		)
	}

	if len(recipient) == 1 {
		return errors.New(
			"Telnyx recipient must contain digits",
		)
	}

	for _, character := range recipient[1:] {
		if character < '0' ||
			character > '9' {
			return errors.New(
				"Telnyx recipient must contain only digits after +",
			)
		}
	}

	body := strings.TrimSpace(
		message.Body,
	)
	if body == "" {
		return errors.New(
			"Telnyx message body is required",
		)
	}

	payload, err := json.Marshal(
		telnyxSendMessageRequest{
			From:               p.from,
			To:                 recipient,
			Text:               body,
			Type:               "SMS",
			MessagingProfileID: p.messagingProfileID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"encode Telnyx request: %w",
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
			"create Telnyx request: %w",
			err,
		)
	}

	request.Header.Set(
		"Authorization",
		"Bearer "+p.apiKey,
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
		return &SMSProviderError{
			Provider: "telnyx",
			Kind:     SMSProviderErrorUnknownDeliveryState,
			Err:      err,
		}
	}

	if response == nil {
		return &SMSProviderError{
			Provider: "telnyx",
			Kind:     SMSProviderErrorUnknownDeliveryState,
		}
	}

	if response.Body != nil {
		defer response.Body.Close()
	}

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		kind := SMSProviderErrorPermanent

		switch {
		case response.StatusCode ==
			http.StatusTooManyRequests:
			kind = SMSProviderErrorRateLimited

		case response.StatusCode >=
			http.StatusInternalServerError:
			kind = SMSProviderErrorTemporary
		}

		return &SMSProviderError{
			Provider:   "telnyx",
			Kind:       kind,
			StatusCode: response.StatusCode,
		}
	}

	return nil
}
func containsLetter(
	value string,
) bool {
	for _, character := range value {
		if character >= 'A' &&
			character <= 'Z' {
			return true
		}

		if character >= 'a' &&
			character <= 'z' {
			return true
		}
	}

	return false
}
