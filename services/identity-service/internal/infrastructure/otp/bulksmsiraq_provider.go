package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type BulkSMSIraqProviderConfig struct {
	Endpoint    string
	OTPEndpoint string
	APIKey      string
	SenderID    string
}

type BulkSMSIraqProvider struct {
	httpClient  HTTPDoer
	endpoint    string
	otpEndpoint string
	apiKey      string
	senderID    string
}

type bulkSMSIraqSendRequest struct {
	Recipient string `json:"recipient"`
	SenderID  string `json:"sender_id"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

type bulkSMSIraqOTPSendRequest struct {
	Recipient string `json:"recipient"`
	SenderID  string `json:"sender_id"`
	Channel   string `json:"channel"`
	Message   string `json:"message"`
	Fallback  string `json:"fallback"`
	Lang      string `json:"lang"`
}

type bulkSMSIraqOTPSendResponse struct {
	Data struct {
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
	} `json:"data"`
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

	otpEndpoint := strings.TrimSpace(
		config.OTPEndpoint,
	)

	if otpEndpoint != "" {
		parsedOTPEndpoint, err := url.ParseRequestURI(
			otpEndpoint,
		)
		if err != nil ||
			parsedOTPEndpoint.Scheme == "" ||
			parsedOTPEndpoint.Host == "" {
			return nil, errors.New(
				"BulkSMSIraq OTP endpoint is invalid",
			)
		}
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
		httpClient:  httpClient,
		endpoint:    endpoint,
		otpEndpoint: otpEndpoint,
		apiKey:      apiKey,
		senderID:    senderID,
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
func (p *BulkSMSIraqProvider) SendTracked(
	ctx context.Context,
	message SMSMessage,
) (SMSProviderDeliveryResult, error) {
	return p.sendOTPTracked(
		ctx,
		message,
		"sms",
	)
}

func (p *BulkSMSIraqProvider) sendOTPTracked(
	ctx context.Context,
	message SMSMessage,
	channel string,
) (SMSProviderDeliveryResult, error) {
	if strings.TrimSpace(p.otpEndpoint) == "" {
		return SMSProviderDeliveryResult{}, errors.New(
			"BulkSMSIraq OTP endpoint is required",
		)
	}

	recipient := strings.TrimSpace(
		message.To,
	)
	if recipient == "" {
		return SMSProviderDeliveryResult{}, errors.New(
			"BulkSMSIraq recipient is required",
		)
	}

	recipient = strings.TrimPrefix(
		recipient,
		"+",
	)

	if recipient == "" {
		return SMSProviderDeliveryResult{}, errors.New(
			"BulkSMSIraq recipient is required",
		)
	}

	for _, character := range recipient {
		if character < '0' ||
			character > '9' {
			return SMSProviderDeliveryResult{}, errors.New(
				"BulkSMSIraq recipient must contain only digits",
			)
		}
	}

	code := strings.TrimSpace(
		message.Code,
	)
	if code == "" {
		return SMSProviderDeliveryResult{}, errors.New(
			"BulkSMSIraq OTP code is required",
		)
	}

	locale := strings.ToLower(
		strings.TrimSpace(
			message.Locale,
		),
	)

	if locale == "" {
		locale = "en"
	}

	switch locale {
	case "en", "ar", "ku":
	default:
		return SMSProviderDeliveryResult{}, fmt.Errorf(
			"BulkSMSIraq OTP locale %q is unsupported",
			locale,
		)
	}

	payload, err := json.Marshal(
		bulkSMSIraqOTPSendRequest{
			Recipient: recipient,
			SenderID:  p.senderID,
			Channel:   channel,
			Message:   code,
			Fallback:  "none",
			Lang:      locale,
		},
	)
	if err != nil {
		return SMSProviderDeliveryResult{}, fmt.Errorf(
			"encode BulkSMSIraq OTP request: %w",
			err,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.otpEndpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return SMSProviderDeliveryResult{}, fmt.Errorf(
			"create BulkSMSIraq OTP request: %w",
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
		return SMSProviderDeliveryResult{},
			&SMSProviderError{
				Provider: "bulksmsiraq",
				Kind:     SMSProviderErrorUnknownDeliveryState,
				Err:      err,
			}
	}

	if response == nil {
		return SMSProviderDeliveryResult{},
			&SMSProviderError{
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
		case response.StatusCode ==
			http.StatusTooManyRequests:
			kind = SMSProviderErrorRateLimited

		case response.StatusCode >=
			http.StatusInternalServerError:
			kind = SMSProviderErrorTemporary
		}

		return SMSProviderDeliveryResult{},
			&SMSProviderError{
				Provider:   "bulksmsiraq",
				Kind:       kind,
				StatusCode: response.StatusCode,
			}
	}

	if response.Body == nil {
		return SMSProviderDeliveryResult{},
			&SMSProviderError{
				Provider: "bulksmsiraq",
				Kind:     SMSProviderErrorUnknownDeliveryState,
			}
	}

	var responsePayload bulkSMSIraqOTPSendResponse

	if err := json.NewDecoder(
		io.LimitReader(
			response.Body,
			64*1024,
		),
	).Decode(
		&responsePayload,
	); err != nil {
		return SMSProviderDeliveryResult{},
			&SMSProviderError{
				Provider: "bulksmsiraq",
				Kind:     SMSProviderErrorUnknownDeliveryState,
				Err:      err,
			}
	}

	requestID := strings.TrimSpace(
		responsePayload.Data.RequestID,
	)

	if requestID == "" {
		return SMSProviderDeliveryResult{},
			&SMSProviderError{
				Provider: "bulksmsiraq",
				Kind:     SMSProviderErrorUnknownDeliveryState,
			}
	}

	return SMSProviderDeliveryResult{
		ProviderMessageID: requestID,
		ProviderStatus: strings.TrimSpace(
			responsePayload.Data.Status,
		),
	}, nil
}
func (p *BulkSMSIraqProvider) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	_, err := p.SendOTPTracked(
		ctx,
		input,
	)

	return err
}

func (p *BulkSMSIraqProvider) SendOTPTracked(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) (WhatsAppProviderDeliveryResult, error) {
	result, err := p.sendOTPTracked(
		ctx,
		SMSMessage{
			ChallengeID: input.ChallengeID,
			To:          input.PhoneNumber,
			Code:        input.Code,
			Locale:      input.Locale,
		},
		"whatsapp",
	)
	if err != nil {
		return WhatsAppProviderDeliveryResult{}, err
	}

	return WhatsAppProviderDeliveryResult{
		ProviderMessageID: result.ProviderMessageID,
		ProviderStatus:    result.ProviderStatus,
	}, nil
}
