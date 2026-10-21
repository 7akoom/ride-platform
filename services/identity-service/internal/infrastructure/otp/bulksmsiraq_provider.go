package otp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// BulkSMSIraqDefaultEndpoint is the gateway's send request (VerifyWay /
// standingtech, API v4). One request sends a plain SMS or a WhatsApp code;
// "type" says which.
const BulkSMSIraqDefaultEndpoint = "https://gateway.standingtech.com/api/v4/sms/send"

type BulkSMSIraqProviderConfig struct {
	Endpoint string
	// OTPEndpoint is where codes go; empty means Endpoint (the gateway has one
	// send request for both).
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

// bulkSMSIraqSendRequest is the gateway's body. For a WhatsApp code, type is
// "whatsapp", message is the code alone and lang picks the template language.
type bulkSMSIraqSendRequest struct {
	Recipient string `json:"recipient"`
	SenderID  string `json:"sender_id"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Lang      string `json:"lang,omitempty"`
}

// bulkSMSIraqResponse is the gateway's answer: {"status":"success","data":{…}}
// or {"status":"failed","error":{"code":"1103","message":"…"}}, possibly with
// HTTP 200 on a failure, so the body decides.
type bulkSMSIraqResponse struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  *struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"error"`
}

// bulkSMSIraqTokenError is the gateway's code for a wrong or missing API key.
const bulkSMSIraqTokenError = "1103"

func NewBulkSMSIraqProvider(
	httpClient HTTPDoer,
	config BulkSMSIraqProviderConfig,
) (*BulkSMSIraqProvider, error) {
	if httpClient == nil {
		return nil, errors.New("BulkSMSIraq HTTP client is required")
	}

	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		return nil, errors.New("BulkSMSIraq endpoint is required")
	}

	if !validHTTPURL(endpoint) {
		return nil, errors.New("BulkSMSIraq endpoint is invalid")
	}

	otpEndpoint := strings.TrimSpace(config.OTPEndpoint)
	if otpEndpoint == "" {
		otpEndpoint = endpoint
	} else if !validHTTPURL(otpEndpoint) {
		return nil, errors.New("BulkSMSIraq OTP endpoint is invalid")
	}

	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("BulkSMSIraq API key is required")
	}

	senderID := strings.TrimSpace(config.SenderID)
	if senderID == "" {
		return nil, errors.New("BulkSMSIraq sender ID is required")
	}

	return &BulkSMSIraqProvider{
		httpClient:  httpClient,
		endpoint:    endpoint,
		otpEndpoint: otpEndpoint,
		apiKey:      apiKey,
		senderID:    senderID,
	}, nil
}

func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)

	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

// Send sends a plain SMS with the rendered text.
func (p *BulkSMSIraqProvider) Send(
	ctx context.Context,
	message SMSMessage,
) error {
	_, err := p.sendPlain(ctx, p.endpoint, message)

	return err
}

// SendTracked sends a code by plain SMS (the rendered text) and returns the
// gateway's id for it.
func (p *BulkSMSIraqProvider) SendTracked(
	ctx context.Context,
	message SMSMessage,
) (SMSProviderDeliveryResult, error) {
	return p.sendPlain(ctx, p.otpEndpoint, message)
}

func (p *BulkSMSIraqProvider) sendPlain(
	ctx context.Context,
	endpoint string,
	message SMSMessage,
) (SMSProviderDeliveryResult, error) {
	recipient, err := bulkSMSIraqRecipient(message.To)
	if err != nil {
		return SMSProviderDeliveryResult{}, err
	}

	body := strings.TrimSpace(message.Body)
	if body == "" {
		return SMSProviderDeliveryResult{}, errors.New("BulkSMSIraq message body is required")
	}

	return p.send(ctx, endpoint, bulkSMSIraqSendRequest{
		Recipient: recipient,
		SenderID:  p.senderID,
		Type:      "plain",
		Message:   body,
	})
}

func (p *BulkSMSIraqProvider) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	_, err := p.SendOTPTracked(ctx, input)

	return err
}

// SendOTPTracked sends a code by WhatsApp: the gateway puts it in its approved
// template in the language asked (en, ar or ku; anything else is en).
func (p *BulkSMSIraqProvider) SendOTPTracked(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) (WhatsAppProviderDeliveryResult, error) {
	recipient, err := bulkSMSIraqRecipient(input.PhoneNumber)
	if err != nil {
		return WhatsAppProviderDeliveryResult{}, err
	}

	code := strings.TrimSpace(input.Code)
	if code == "" {
		return WhatsAppProviderDeliveryResult{}, errors.New("BulkSMSIraq OTP code is required")
	}

	result, err := p.send(ctx, p.otpEndpoint, bulkSMSIraqSendRequest{
		Recipient: recipient,
		SenderID:  p.senderID,
		Type:      "whatsapp",
		Message:   code,
		Lang:      bulkSMSIraqLanguage(input.Locale),
	})
	if err != nil {
		return WhatsAppProviderDeliveryResult{}, err
	}

	return WhatsAppProviderDeliveryResult{
		ProviderMessageID: result.ProviderMessageID,
		ProviderStatus:    result.ProviderStatus,
	}, nil
}

func bulkSMSIraqLanguage(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if index := strings.IndexAny(locale, "-_"); index > 0 {
		locale = locale[:index]
	}

	switch locale {
	case "ar", "ku":
		return locale
	default:
		return "en"
	}
}

// bulkSMSIraqRecipient is the number in the gateway's form: digits only, with
// the country code and no plus (9647XXXXXXXXX).
func bulkSMSIraqRecipient(value string) (string, error) {
	recipient := strings.TrimPrefix(strings.TrimSpace(value), "+")
	if recipient == "" {
		return "", errors.New("BulkSMSIraq recipient is required")
	}

	for _, character := range recipient {
		if character < '0' || character > '9' {
			return "", errors.New("BulkSMSIraq recipient must contain only digits")
		}
	}

	return recipient, nil
}

func (p *BulkSMSIraqProvider) send(
	ctx context.Context,
	endpoint string,
	body bulkSMSIraqSendRequest,
) (SMSProviderDeliveryResult, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return SMSProviderDeliveryResult{}, fmt.Errorf("encode BulkSMSIraq request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return SMSProviderDeliveryResult{}, fmt.Errorf("create BulkSMSIraq request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return SMSProviderDeliveryResult{}, &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorUnknownDeliveryState,
			Err:      err,
		}
	}

	if response == nil {
		return SMSProviderDeliveryResult{}, &SMSProviderError{
			Provider: "bulksmsiraq",
			Kind:     SMSProviderErrorUnknownDeliveryState,
		}
	}

	var answer bulkSMSIraqResponse

	decoded := false

	if response.Body != nil {
		defer response.Body.Close()

		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		if readErr == nil && json.Unmarshal(raw, &answer) == nil {
			decoded = true
		}
	}

	status := strings.ToLower(strings.TrimSpace(answer.Status))
	errorCode, errorMessage := answer.errorDetails()
	failed := status == "failed" || status == "error" || errorCode != "" || errorMessage != ""

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || failed {
		return SMSProviderDeliveryResult{}, &SMSProviderError{
			Provider:   "bulksmsiraq",
			Kind:       bulkSMSIraqFailureKind(response.StatusCode),
			StatusCode: response.StatusCode,
			Err:        bulkSMSIraqGatewayError(errorCode, errorMessage),
		}
	}

	// A 2xx the gateway did not explain: it may or may not have sent it.
	if !decoded {
		return SMSProviderDeliveryResult{}, &SMSProviderError{
			Provider:   "bulksmsiraq",
			Kind:       SMSProviderErrorUnknownDeliveryState,
			StatusCode: response.StatusCode,
			Err:        errors.New("BulkSMSIraq answered without a JSON body"),
		}
	}

	messageID := answer.messageID()
	if messageID == "" {
		// Delivery tracking needs an id of its own for every attempt.
		messageID = "bulksmsiraq-" + randomHex(12)
	}

	status = strings.TrimSpace(answer.Status)
	if status == "" {
		status = "accepted"
	}

	return SMSProviderDeliveryResult{
		ProviderMessageID: messageID,
		ProviderStatus:    status,
	}, nil
}

func (r bulkSMSIraqResponse) errorDetails() (string, string) {
	if r.Error == nil {
		return "", ""
	}

	return jsonScalar(r.Error.Code), strings.TrimSpace(r.Error.Message)
}

// messageID finds the gateway's id for the message, wherever its data has it.
func (r bulkSMSIraqResponse) messageID() string {
	if len(r.Data) == 0 {
		return ""
	}

	var data map[string]json.RawMessage

	if json.Unmarshal(r.Data, &data) != nil {
		// Some answers carry a list with one message.
		var list []map[string]json.RawMessage
		if json.Unmarshal(r.Data, &list) != nil || len(list) == 0 {
			return ""
		}

		data = list[0]
	}

	for _, key := range []string{"uid", "message_id", "request_id", "id", "sms_id"} {
		if id := jsonScalar(data[key]); id != "" && len(id) <= 200 {
			return id
		}
	}

	return ""
}

func bulkSMSIraqFailureKind(statusCode int) SMSProviderErrorKind {
	switch {
	case statusCode == http.StatusTooManyRequests:
		return SMSProviderErrorRateLimited
	case statusCode >= http.StatusInternalServerError:
		return SMSProviderErrorTemporary
	default:
		// A wrong key, a sender not approved, no balance, a bad number: trying
		// again would not help.
		return SMSProviderErrorPermanent
	}
}

func bulkSMSIraqGatewayError(code string, message string) error {
	switch {
	case code == bulkSMSIraqTokenError:
		return fmt.Errorf("BulkSMSIraq error %s: the API key is wrong or missing (BULKSMSIRAQ_API_KEY)", code)
	case code != "" && message != "":
		return fmt.Errorf("BulkSMSIraq error %s: %s", code, message)
	case code != "":
		return fmt.Errorf("BulkSMSIraq error %s", code)
	case message != "":
		return fmt.Errorf("BulkSMSIraq: %s", message)
	default:
		return nil
	}
}

// jsonScalar is a JSON string or number as text; anything else is empty.
func jsonScalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}

	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}

	return ""
}

func randomHex(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "unknown"
	}

	return hex.EncodeToString(buffer)
}
