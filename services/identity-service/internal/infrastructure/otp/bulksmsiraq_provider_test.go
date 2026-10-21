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

func bulkSMSIraqTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newTestBulkSMSIraqProvider(t *testing.T, doer *testHTTPDoer) *BulkSMSIraqProvider {
	t.Helper()

	provider, err := NewBulkSMSIraqProvider(doer, BulkSMSIraqProviderConfig{
		Endpoint: BulkSMSIraqDefaultEndpoint,
		APIKey:   "test-key",
		SenderID: "LindaAgency",
	})
	if err != nil {
		t.Fatalf("NewBulkSMSIraqProvider: %v", err)
	}

	return provider
}

func decodeBulkSMSIraqTestRequest(t *testing.T, request *http.Request) map[string]any {
	t.Helper()

	if request == nil {
		t.Fatal("no request was sent")
	}

	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	return body
}

func TestNewBulkSMSIraqProviderAcceptsValidConfiguration(t *testing.T) {
	provider := newTestBulkSMSIraqProvider(t, &testHTTPDoer{})

	if provider.otpEndpoint != BulkSMSIraqDefaultEndpoint {
		t.Fatalf("the code endpoint is %q, want the send endpoint", provider.otpEndpoint)
	}
}

func TestNewBulkSMSIraqProviderRejectsInvalidConfiguration(t *testing.T) {
	valid := BulkSMSIraqProviderConfig{
		Endpoint: BulkSMSIraqDefaultEndpoint,
		APIKey:   "key",
		SenderID: "LindaAgency",
	}

	tests := map[string]func(*BulkSMSIraqProviderConfig){
		"no endpoint":       func(c *BulkSMSIraqProviderConfig) { c.Endpoint = " " },
		"bad endpoint":      func(c *BulkSMSIraqProviderConfig) { c.Endpoint = "gateway" },
		"bad code endpoint": func(c *BulkSMSIraqProviderConfig) { c.OTPEndpoint = "gateway" },
		"no API key":        func(c *BulkSMSIraqProviderConfig) { c.APIKey = "" },
		"no sender":         func(c *BulkSMSIraqProviderConfig) { c.SenderID = "" },
	}

	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			config := valid
			change(&config)

			if _, err := NewBulkSMSIraqProvider(&testHTTPDoer{}, config); err == nil {
				t.Fatal("the configuration was accepted")
			}
		})
	}

	if _, err := NewBulkSMSIraqProvider(nil, valid); err == nil {
		t.Fatal("a nil HTTP client was accepted")
	}
}

func TestBulkSMSIraqProviderSendsWhatsAppCode(t *testing.T) {
	doer := &testHTTPDoer{
		response: bulkSMSIraqTestResponse(http.StatusOK, `{"status":"success","data":{"uid":"6721a3f0b1c2d","status":"pending"}}`),
	}
	provider := newTestBulkSMSIraqProvider(t, doer)

	result, err := provider.SendOTPTracked(context.Background(), WhatsAppOTPProviderInput{
		ChallengeID: "challenge-1",
		PhoneNumber: "+9647501234567",
		Code:        "123654",
		Purpose:     auth.OTPPurposeLogin,
		Locale:      "ar-IQ",
	})
	if err != nil {
		t.Fatalf("SendOTPTracked: %v", err)
	}

	if result.ProviderMessageID != "6721a3f0b1c2d" || result.ProviderStatus != "success" {
		t.Fatalf("result = %+v", result)
	}

	request := doer.request
	if request.Method != http.MethodPost || request.URL.String() != BulkSMSIraqDefaultEndpoint {
		t.Fatalf("request = %s %s", request.Method, request.URL)
	}

	if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q", got)
	}

	if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("Accept") != "application/json" {
		t.Fatalf("headers = %v", request.Header)
	}

	body := decodeBulkSMSIraqTestRequest(t, request)
	want := map[string]any{
		"recipient": "9647501234567",
		"sender_id": "LindaAgency",
		"type":      "whatsapp",
		"message":   "123654",
		"lang":      "ar",
	}

	if len(body) != len(want) {
		t.Fatalf("body = %v, want %v", body, want)
	}

	for key, value := range want {
		if body[key] != value {
			t.Fatalf("body[%s] = %v, want %v (body %v)", key, body[key], value, body)
		}
	}
}

func TestBulkSMSIraqProviderCodeLanguages(t *testing.T) {
	for locale, want := range map[string]string{
		"":      "en",
		"en":    "en",
		"ar":    "ar",
		"AR_iq": "ar",
		"ku":    "ku",
		"ckb":   "en",
		"fr":    "en",
	} {
		if got := bulkSMSIraqLanguage(locale); got != want {
			t.Fatalf("bulkSMSIraqLanguage(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestBulkSMSIraqProviderSendsPlainSMS(t *testing.T) {
	doer := &testHTTPDoer{response: bulkSMSIraqTestResponse(http.StatusOK, `{"status":"success","data":{"id":991}}`)}
	provider := newTestBulkSMSIraqProvider(t, doer)

	result, err := provider.SendTracked(context.Background(), SMSMessage{
		To:   "+9647501234567",
		Body: "Your Ride code is 123654",
		Code: "123654",
	})
	if err != nil {
		t.Fatalf("SendTracked: %v", err)
	}

	if result.ProviderMessageID != "991" {
		t.Fatalf("message id = %q", result.ProviderMessageID)
	}

	body := decodeBulkSMSIraqTestRequest(t, doer.request)
	if body["type"] != "plain" || body["message"] != "Your Ride code is 123654" || body["lang"] != nil {
		t.Fatalf("body = %v", body)
	}
}

func TestBulkSMSIraqProviderGivesAnIDWhenTheGatewayHasNone(t *testing.T) {
	doer := &testHTTPDoer{response: bulkSMSIraqTestResponse(http.StatusOK, `{"status":"success","message":"sent"}`)}
	provider := newTestBulkSMSIraqProvider(t, doer)

	first, err := provider.SendOTPTracked(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "1"})
	if err != nil {
		t.Fatalf("SendOTPTracked: %v", err)
	}

	doer.response = bulkSMSIraqTestResponse(http.StatusOK, `{"status":"success"}`)

	second, err := provider.SendOTPTracked(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "2"})
	if err != nil {
		t.Fatalf("SendOTPTracked: %v", err)
	}

	if !strings.HasPrefix(first.ProviderMessageID, "bulksmsiraq-") || first.ProviderMessageID == second.ProviderMessageID {
		t.Fatalf("ids = %q, %q", first.ProviderMessageID, second.ProviderMessageID)
	}
}

func TestBulkSMSIraqProviderFailedBodyIsAFailureEvenWithHTTP200(t *testing.T) {
	doer := &testHTTPDoer{
		response: bulkSMSIraqTestResponse(http.StatusOK, `{"status":"failed","error":{"code":"1103","message":"Invalid or missing token"}}`),
	}
	provider := newTestBulkSMSIraqProvider(t, doer)

	_, err := provider.SendOTPTracked(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "1"})

	var providerErr *SMSProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != SMSProviderErrorPermanent {
		t.Fatalf("err = %v", err)
	}

	if !strings.Contains(err.Error(), "1103") || !strings.Contains(err.Error(), "BULKSMSIRAQ_API_KEY") {
		t.Fatalf("the error does not say what to fix: %v", err)
	}

	if strings.Contains(err.Error(), "test-key") {
		t.Fatalf("the error has the API key: %v", err)
	}
}

func TestBulkSMSIraqProviderClassifiesHTTPFailures(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   SMSProviderErrorKind
	}{
		{http.StatusUnauthorized, `{"status":"failed","error":{"code":1103,"message":"Invalid or missing token"}}`, SMSProviderErrorPermanent},
		{http.StatusUnprocessableEntity, `{"status":"failed","error":{"code":"1002","message":"Insufficient balance"}}`, SMSProviderErrorPermanent},
		{http.StatusTooManyRequests, ``, SMSProviderErrorRateLimited},
		{http.StatusBadGateway, `<html>bad gateway</html>`, SMSProviderErrorTemporary},
	}

	for _, test := range tests {
		doer := &testHTTPDoer{response: bulkSMSIraqTestResponse(test.status, test.body)}
		provider := newTestBulkSMSIraqProvider(t, doer)

		_, err := provider.SendOTPTracked(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "1"})

		var providerErr *SMSProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != test.want || providerErr.StatusCode != test.status {
			t.Fatalf("HTTP %d: err = %v, want %s", test.status, err, test.want)
		}
	}
}

func TestBulkSMSIraqProviderUnreadableSuccessIsUnknown(t *testing.T) {
	doer := &testHTTPDoer{response: bulkSMSIraqTestResponse(http.StatusOK, `OK`)}
	provider := newTestBulkSMSIraqProvider(t, doer)

	_, err := provider.SendOTPTracked(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "1"})

	var providerErr *SMSProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != SMSProviderErrorUnknownDeliveryState {
		t.Fatalf("err = %v", err)
	}
}

func TestBulkSMSIraqProviderNetworkErrorIsUnknown(t *testing.T) {
	doer := &testHTTPDoer{err: errors.New("connection reset")}
	provider := newTestBulkSMSIraqProvider(t, doer)

	err := provider.SendOTP(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "1"})

	var providerErr *SMSProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != SMSProviderErrorUnknownDeliveryState {
		t.Fatalf("err = %v", err)
	}

	doer = &testHTTPDoer{}
	provider = newTestBulkSMSIraqProvider(t, doer)

	err = provider.SendOTP(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "9647501234567", Code: "1"})
	if !errors.As(err, &providerErr) || providerErr.Kind != SMSProviderErrorUnknownDeliveryState {
		t.Fatalf("nil response: err = %v", err)
	}
}

func TestBulkSMSIraqProviderRejectsBadInputWithoutSending(t *testing.T) {
	doer := &testHTTPDoer{}
	provider := newTestBulkSMSIraqProvider(t, doer)
	ctx := context.Background()

	for name, err := range map[string]error{
		"no number":      provider.SendOTP(ctx, WhatsAppOTPProviderInput{Code: "1"}),
		"letters":        provider.SendOTP(ctx, WhatsAppOTPProviderInput{PhoneNumber: "+964 750", Code: "1"}),
		"no code":        provider.SendOTP(ctx, WhatsAppOTPProviderInput{PhoneNumber: "9647501234567"}),
		"no SMS text":    provider.Send(ctx, SMSMessage{To: "9647501234567"}),
		"SMS bad number": provider.Send(ctx, SMSMessage{To: "abc", Body: "hi"}),
	} {
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}

	if doer.calls != 0 {
		t.Fatalf("%d requests were sent", doer.calls)
	}
}
