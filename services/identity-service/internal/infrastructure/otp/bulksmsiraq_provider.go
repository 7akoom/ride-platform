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

type BulkSMSIraqProviderConfig struct {
	Endpoint string
	APIKey   string
	SenderID string
}

type BulkSMSIraqProvider struct {
	httpClient HTTPDoer
	endpoint   string
	apiKey     string
	senderID   string
}

type bulkSMSIraqSendRequest struct {
	Recipient string `json:"recipient"`
	SenderID  string `json:"sender_id"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

func NewBulkSMSIraqProvider(
	httpClient HTTPDoer,
	config BulkSMSIraqProviderConfig,
) (*BulkSMSIraqProvider, error) {
	if httpClient == nil {
		return nil, errors.New(
			"BulkSMSIraq HTTP client is required",
		)
	}

	endpoint := strings.TrimSpace(
		config.Endpoint,
	)
	if endpoint == "" {
		return nil, errors.New(
			"BulkSMSIraq endpoint is required",
		)
	}

	parsedEndpoint, err := url.ParseRequestURI(
		endpoint,
	)
	if err != nil ||
		parsedEndpoint.Scheme == "" ||
		parsedEndpoint.Host == "" {
		return nil, errors.New(
			"BulkSMSIraq endpoint is invalid",
		)
	}

	apiKey := strings.TrimSpace(
		config.APIKey,
	)
	if apiKey == "" {
		return nil, errors.New(
			"BulkSMSIraq API key is required",
		)
	}

	senderID := strings.TrimSpace(
		config.SenderID,
	)
	if senderID == "" {
		return nil, errors.New(
			"BulkSMSIraq sender ID is required",
		)
	}

	return &BulkSMSIraqProvider{
		httpClient: httpClient,
		endpoint:   endpoint,
		apiKey:     apiKey,
		senderID:   senderID,
	}, nil
}

func (p *BulkSMSIraqProvider) Send(
	ctx context.Context,
	message SMSMessage,
) error {
	recipient := strings.TrimSpace(
		message.To,
	)
	if recipient == "" {
		return errors.New(
			"BulkSMSIraq recipient is required",
		)
	}

	recipient = strings.TrimPrefix(
		recipient,
		"+",
	)

	for _, character := range recipient {
		if character < '0' || character > '9' {
			return errors.New(
				"BulkSMSIraq recipient must contain only digits",
			)
		}
	}

	body := strings.TrimSpace(
		message.Body,
	)
	if body == "" {
		return errors.New(
			"BulkSMSIraq message body is required",
		)
	}

	payload, err := json.Marshal(
		bulkSMSIraqSendRequest{
			Recipient: recipient,
			SenderID:  p.senderID,
			Type:      "plain",
			Message:   body,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"encode BulkSMSIraq request: %w",
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
			"create BulkSMSIraq request: %w",
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
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorUnknownDeliveryState,
			Err:      err,
		}
	}

	if response == nil {
		return &SMSProviderError{
			Provider: "bulksmsiraq",
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
		case response.StatusCode == http.StatusTooManyRequests:
			kind = SMSProviderErrorRateLimited

		case response.StatusCode >= http.StatusInternalServerError:
			kind = SMSProviderErrorTemporary
		}

		return &SMSProviderError{
			Provider:   "bulksmsiraq",
			Kind:       kind,
			StatusCode: response.StatusCode,
		}
	}

	return nil
}
