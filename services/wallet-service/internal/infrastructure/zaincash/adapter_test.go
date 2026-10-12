package zaincash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// signed is a token shaped like the docs' "Decoded Token Example": the
// payment under data.
func signed(t *testing.T, secret, status string) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"eventType": "STATUS_CHANGED",
		"eventId":   "812691e6-b433-4ffc-888c-d11e21c14994",
		"timestamp": "2023-10-27T10:15:30.000+00:00",
		"data": map[string]any{
			"transactionId":       "zc-1",
			"merchantReferenceId": "ref-1",
			"customerMsisdn":      "9647XXXXXXXXX",
			"orderId":             "order-1",
			"operationId":         1252738043970122,
			"serviceType":         "rider_topup",
			"language":            "ar",
			"errorMessage":        nil,
			"previousStatus":      "PENDING",
			"currentStatus":       status,
			"amount":              map[string]any{"currency": "IQD", "value": 5000, "feeValue": 0},
		},
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	return token
}

func TestZainCashStatusesBecomeOutcomes(t *testing.T) {
	adapter := NewAdapter(NewClient(Config{WebhookSecret: "api-key"}))

	for status, want := range map[string]topup.Outcome{
		"SUCCESS":                          topup.OutcomeSucceeded,
		"FAILED":                           topup.OutcomeFailed,
		"EXPIRED":                          topup.OutcomeFailed,
		"REFUNDED":                         topup.OutcomeFailed,
		"PENDING":                          topup.OutcomePending,
		"OTP_SENT":                         topup.OutcomePending,
		"CUSTOMER_AUTHENTICATION_REQUIRED": topup.OutcomePending,
		"SOMETHING_NEW":                    topup.OutcomePending,
	} {
		notice, err := adapter.Verify(signed(t, "api-key", status))
		if err != nil || notice.Outcome != want || notice.ReferenceID != "ref-1" || notice.ProviderTransactionID != "zc-1" {
			t.Errorf("%s: %+v %v", status, notice, err)
		}
	}

	if _, err := adapter.Verify(signed(t, "someone-else", "SUCCESS")); err == nil {
		t.Fatal("a token signed with another key was accepted")
	}

	if _, err := NewAdapter(NewClient(Config{})).Verify(signed(t, "any", "SUCCESS")); err == nil {
		t.Fatal("a token was accepted with no API key set")
	}
}

func TestATokenNamingNoPaymentIsRefused(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		// The old guess: the payment at the top level, not under data.
		"merchantReferenceId": "ref-1",
		"currentStatus":       "SUCCESS",
	}).SignedString([]byte("api-key"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewAdapter(NewClient(Config{WebhookSecret: "api-key"})).Verify(token); err == nil {
		t.Fatal("a token without data was accepted")
	}
}

func TestZainCashTakesWholeDinarsOnly(t *testing.T) {
	adapter := NewAdapter(NewClient(Config{}))

	_, err := adapter.Start(context.Background(), topup.StartInput{
		OwnerType: wallet.OwnerRider, Amount: decimal.RequireFromString("1000.5"),
	})
	if !errors.Is(err, topup.ErrAmountNotSupported) {
		t.Fatalf("a fraction: %v", err)
	}
}

func TestTheLanguageIsOneZainCashSpeaks(t *testing.T) {
	for in, want := range map[string]string{"ar": "ar", "EN": "en", "ku": "ku", "ckb": "ku", "": "ar", "fr": "ar"} {
		if got := languageOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// fakeZainCash answers like the docs' examples.
type fakeZainCash struct {
	mu       sync.Mutex
	tokens   int
	init     map[string]any
	inquired []string
	status   string
}

func (f *fakeZainCash) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))

		if form.Get("grant_type") != "client_credentials" || form.Get("client_id") != "client" || form.Get("client_secret") != "secret" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)

			return
		}

		f.tokens++
		_, _ = io.WriteString(w, `{"access_token":"access-1","token_type":"Bearer","expires_in":3600}`)

	case r.Header.Get("Authorization") != "Bearer access-1":
		http.Error(w, "unauthorized", http.StatusUnauthorized)

	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/payment-gateway/transaction/init":
		_ = json.NewDecoder(r.Body).Decode(&f.init)
		_, _ = io.WriteString(w, `{
			"status": "SUCCESS",
			"transactionDetails": {
				"transactionId": "9da792d5-f818-4e98-9fb6-6c0b13902c8b",
				"externalReferenceId": "d8594f04-cfcc-4fc3-b901-01513cc697bc",
				"orderId": "moi-21323123vc",
				"amount": {"currency": "IQD", "value": 500}
			},
			"redirectUrl": "https://pg-api-uat.zaincash.iq/transaction/pay?id=transactionId&token=token_value",
			"expiryTime": "2025-12-22T08:04:27.402+00:00",
			"createdAt": "2025-12-22T07:49:28.540+00:00"
		}`)

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/payment-gateway/transaction/inquiry/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v2/payment-gateway/transaction/inquiry/")
		f.inquired = append(f.inquired, id)
		_, _ = io.WriteString(w, `{
			"status": "`+f.status+`",
			"transactionDetails": {
				"transactionId": "`+id+`",
				"operationId": null,
				"externalReferenceId": "d8594f04-cfcc-4fc3-b901-01513cc697bc",
				"orderId": "moi-21323123vc",
				"amount": {"currency": "IQD", "value": 500, "feeValue": 0}
			},
			"customer": {"phone": "964XXXXXXXXXX"},
			"timeStamps": {"expiryTime": "2025-12-22T08:04:27.403+00:00", "completedAt": null}
		}`)

	default:
		http.NotFound(w, r)
	}
}

func (f *fakeZainCash) setStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.status = status
}

// seen is what the fake was sent: the init body, how many tokens it gave,
// and the transactions asked about.
func (f *fakeZainCash) seen() (map[string]any, int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.init, f.tokens, append([]string(nil), f.inquired...)
}

func TestAPaymentOpensAndIsAskedAbout(t *testing.T) {
	fake := &fakeZainCash{status: "OTP_SENT"}
	server := httptest.NewServer(fake)
	defer server.Close()

	adapter := NewAdapter(NewClient(Config{BaseURL: server.URL + "/", ClientID: "client", ClientSecret: "secret", Scope: "payment:read payment:write"}))
	ctx := context.Background()

	started, err := adapter.Start(ctx, topup.StartInput{
		ReferenceID: "d8594f04-cfcc-4fc3-b901-01513cc697bc",
		OrderID:     "moi-21323123vc",
		OwnerType:   wallet.OwnerRider,
		Amount:      decimal.NewFromInt(500),
		Language:    "ku",
		SuccessURL:  "https://api.example/pay/done/",
		FailureURL:  "https://api.example/pay/failed/",
	})
	if err != nil {
		t.Fatal(err)
	}

	if started.ProviderTransactionID != "9da792d5-f818-4e98-9fb6-6c0b13902c8b" || !strings.HasPrefix(started.RedirectURL, "https://pg-api-uat.zaincash.iq/transaction/pay") {
		t.Fatalf("started %+v", started)
	}

	body, _, _ := fake.seen()
	amount, _ := body["amount"].(map[string]any)
	urls, _ := body["redirectUrls"].(map[string]any)

	if body["language"] != "ku" || body["externalReferenceId"] != "d8594f04-cfcc-4fc3-b901-01513cc697bc" ||
		body["serviceType"] != "rider_topup" || amount["value"] != "500" || amount["currency"] != "IQD" ||
		urls["successUrl"] != "https://api.example/pay/done/" || urls["failureUrl"] != "https://api.example/pay/failed/" {
		t.Fatalf("init body %+v", body)
	}

	notice, err := adapter.Check(ctx, started.ProviderTransactionID)
	if err != nil || notice.Outcome != topup.OutcomePending || notice.ReferenceID != "d8594f04-cfcc-4fc3-b901-01513cc697bc" {
		t.Fatalf("pending: %+v %v", notice, err)
	}

	fake.setStatus("SUCCESS")

	notice, err = adapter.Check(ctx, started.ProviderTransactionID)
	if err != nil || notice.Outcome != topup.OutcomeSucceeded || notice.ProviderTransactionID != started.ProviderTransactionID {
		t.Fatalf("paid: %+v %v", notice, err)
	}

	if _, tokens, inquired := fake.seen(); tokens != 1 || len(inquired) != 2 || inquired[0] != started.ProviderTransactionID {
		t.Fatalf("one token, two inquiries: %d %v", tokens, inquired)
	}
}

func TestWrongCredentialsAreAnError(t *testing.T) {
	server := httptest.NewServer(&fakeZainCash{status: "SUCCESS"})
	defer server.Close()

	adapter := NewAdapter(NewClient(Config{BaseURL: server.URL, ClientID: "client", ClientSecret: "wrong"}))

	if _, err := adapter.Check(context.Background(), "zc-1"); err == nil {
		t.Fatal("an inquiry with wrong credentials worked")
	}
}

func TestAnInitAnswerWithoutATransactionIsAnError(t *testing.T) {
	if _, err := decodeInit([]byte(`{"transactionId":"top-level","redirectUrl":"https://pay"}`)); err == nil {
		t.Fatal("the old guess (transactionId at the top level) was read")
	}
}

func TestConfiguredNeedsTheClientCredentials(t *testing.T) {
	for config, want := range map[Config]bool{
		{}:                                      false,
		{ClientID: "client"}:                    false,
		{ClientID: "client", ClientSecret: " "}: false,
		{ClientID: "client", ClientSecret: "s"}: true,
		{WebhookSecret: "only the API key is set"}: false,
	} {
		if got := config.Configured(); got != want {
			t.Errorf("%+v: got %v, want %v", config, got, want)
		}
	}
}
