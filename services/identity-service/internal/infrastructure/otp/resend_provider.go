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

type ResendProviderConfig struct {
	Endpoint string
	APIKey   string
	From     string
}

type ResendProvider struct {
	httpClient HTTPDoer
	endpoint   string
	apiKey     string
	from       string
}

type resendSendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

func NewResendProvider(
	httpClient HTTPDoer,
	config ResendProviderConfig,
) (*ResendProvider, error) {
	if httpClient == nil {
		return nil, errors.New(
			"Resend HTTP client is required",
		)
	}

	endpoint := strings.TrimSpace(
		config.Endpoint,
	)
	if endpoint == "" {
		return nil, errors.New(
			"Resend endpoint is required",
		)
	}

	parsedEndpoint, err := url.ParseRequestURI(
		endpoint,
	)
	if err != nil ||
		parsedEndpoint.Scheme == "" ||
		parsedEndpoint.Host == "" {
		return nil, errors.New(
			"Resend endpoint is invalid",
		)
	}

	apiKey := strings.TrimSpace(
		config.APIKey,
	)
	if apiKey == "" {
		return nil, errors.New(
			"Resend API key is required",
		)
	}

	from := strings.TrimSpace(
		config.From,
	)
	if from == "" {
		return nil, errors.New(
			"Resend sender is required",
		)
	}

	return &ResendProvider{
		httpClient: httpClient,
		endpoint:   endpoint,
		apiKey:     apiKey,
		from:       from,
	}, nil
}

func (p *ResendProvider) Send(
	ctx context.Context,
	message EmailMessage,
) error {
	recipient := strings.TrimSpace(
		message.To,
	)
	if recipient == "" {
		return errors.New(
			"Resend recipient is required",
		)
	}

	subject := strings.TrimSpace(
		message.Subject,
	)
	if subject == "" {
		return errors.New(
			"Resend subject is required",
		)
	}

	textBody := strings.TrimSpace(
		message.TextBody,
	)

	htmlBody := strings.TrimSpace(
		message.HTMLBody,
	)

	if textBody == "" &&
		htmlBody == "" {
		return errors.New(
			"Resend message body is required",
		)
	}

	payload, err := json.Marshal(
		resendSendEmailRequest{
			From: p.from,
			To: []string{
				recipient,
			},
			Subject: subject,
			HTML:    htmlBody,
			Text:    textBody,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"encode Resend request: %w",
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
			"create Resend request: %w",
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
		return &EmailProviderError{
			Provider: "resend",
			Kind:     EmailProviderErrorUnknownDeliveryState,
			Err:      err,
		}
	}

	if response == nil {
		return &EmailProviderError{
			Provider: "resend",
			Kind:     EmailProviderErrorUnknownDeliveryState,
		}
	}

	if response.Body != nil {
		defer response.Body.Close()
	}

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		kind := EmailProviderErrorPermanent

		switch {
		case response.StatusCode ==
			http.StatusTooManyRequests:
			kind = EmailProviderErrorRateLimited

		case response.StatusCode >=
			http.StatusInternalServerError:
			kind = EmailProviderErrorTemporary
		}

		return &EmailProviderError{
			Provider:   "resend",
			Kind:       kind,
			StatusCode: response.StatusCode,
		}
	}

	return nil
}
