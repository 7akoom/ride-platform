// Package zaincash is a thin client for the ZainCash Payment Gateway v2
// (https://docs.zaincash.iq): OAuth2 client-credentials auth, creating a
// payment session, asking where a payment stands (Transaction Inquiry), and
// verifying the HS256 JWT ZainCash sends back on both the redirect callback
// and the webhook. It knows nothing about wallets or drivers — that
// orchestration lives in internal/application/topup.
//
// The shapes here follow the docs' own examples (checked 12 October 2026):
// the init answer carries the transaction under transactionDetails, and a
// callback or webhook token carries it under data.
package zaincash

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
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	// https://pg-api-uat.zaincash.iq for the test environment; the
	// production address comes with onboarding.
	BaseURL      string
	ClientID     string
	ClientSecret string
	// Space-separated, e.g. "payment:read payment:write".
	Scope string
	// ZainCash's "API Secret Key": it signs the redirect callback's and the
	// webhook's HS256 JWT. A separate value from ClientSecret.
	WebhookSecret string
}

// Configured is true when the client can open payments: the OAuth2 client
// credentials are set.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

type Client struct {
	httpClient *http.Client
	config     Config

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func NewClient(config Config) *Client {
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")

	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		config:     config,
	}
}

// token returns a cached OAuth2 access token, refreshing it via
// client_credentials shortly before it actually expires.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.expiresAt) {
		return c.accessToken, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
		"scope":         {c.config.Scope},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.config.BaseURL+"/oauth2/token",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("zaincash: build oauth2 token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("zaincash: oauth2 token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("zaincash: read oauth2 token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zaincash: oauth2 token request failed: %s: %s", resp.Status, snippet(body))
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("zaincash: decode oauth2 token response: %w", err)
	}

	// The body holds the token itself, so it is never quoted here.
	if tokenResponse.AccessToken == "" {
		return "", errors.New("zaincash: the oauth2 token response has no access_token")
	}

	lifetime := time.Duration(tokenResponse.ExpiresIn) * time.Second
	if lifetime <= time.Minute {
		// Unknown or very short: ask again next time rather than risk an
		// expired token.
		lifetime = time.Minute
	}

	c.accessToken = tokenResponse.AccessToken
	// Refresh 30s early so a request in flight never races the actual
	// expiry.
	c.expiresAt = time.Now().Add(lifetime - 30*time.Second)

	return c.accessToken, nil
}

type InitTransactionInput struct {
	// "en", "ar" or "ku": the language of ZainCash's payment page.
	Language string

	// Our own idempotency key — becomes ZainCash's externalReferenceId and
	// comes back as merchantReferenceId.
	ExternalReferenceID string

	OrderID string
	// Shown on the merchant dashboard; no "+" or ",".
	ServiceType string

	// Whole IQD amount as a decimal string.
	AmountValue string

	// Optional, international format (9647...). Omitted: ZainCash's page
	// asks for the wallet number itself.
	CustomerPhone string

	SuccessURL string
	FailureURL string
}

type InitTransactionResult struct {
	TransactionID string
	RedirectURL   string
	// When ZainCash stops taking the payment; zero when it did not say.
	ExpiresAt time.Time
}

func (c *Client) InitTransaction(
	ctx context.Context,
	input InitTransactionInput,
) (InitTransactionResult, error) {
	payload := map[string]any{
		"language":            input.Language,
		"externalReferenceId": input.ExternalReferenceID,
		"orderId":             input.OrderID,
		"serviceType":         input.ServiceType,
		"amount": map[string]any{
			"value":    input.AmountValue,
			"currency": "IQD",
		},
		"redirectUrls": map[string]any{
			"successUrl": input.SuccessURL,
			"failureUrl": input.FailureURL,
		},
	}

	if strings.TrimSpace(input.CustomerPhone) != "" {
		payload["customer"] = map[string]any{"phone": input.CustomerPhone}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: encode init request: %w", err)
	}

	respBody, err := c.call(ctx, http.MethodPost, "/api/v2/payment-gateway/transaction/init", body)
	if err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: init: %w", err)
	}

	return decodeInit(respBody)
}

// decodeInit reads the init answer:
//
//	{"status": "SUCCESS",
//	 "transactionDetails": {"transactionId": "...", "externalReferenceId": "...", ...},
//	 "redirectUrl": "https://.../transaction/pay?id=...&token=...",
//	 "expiryTime": "2025-12-22T08:04:27.402+00:00", ...}
func decodeInit(body []byte) (InitTransactionResult, error) {
	var answer struct {
		TransactionDetails struct {
			TransactionID string `json:"transactionId"`
		} `json:"transactionDetails"`
		RedirectURL string `json:"redirectUrl"`
		ExpiryTime  string `json:"expiryTime"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: decode init response: %w", err)
	}

	result := InitTransactionResult{
		TransactionID: strings.TrimSpace(answer.TransactionDetails.TransactionID),
		RedirectURL:   strings.TrimSpace(answer.RedirectURL),
		ExpiresAt:     parseTime(answer.ExpiryTime),
	}

	if result.TransactionID == "" || result.RedirectURL == "" {
		return InitTransactionResult{}, fmt.Errorf("zaincash: the init response has no transaction id or redirect url: %s", snippet(body))
	}

	return result, nil
}

// Inquiry is where ZainCash says a payment stands.
type Inquiry struct {
	// SUCCESS, FAILED, PENDING, OTP_SENT, CUSTOMER_AUTHENTICATION_REQUIRED,
	// EXPIRED or REFUNDED.
	Status              string
	TransactionID       string
	ExternalReferenceID string
}

// Inquire asks ZainCash where the payment with transactionID (from init)
// stands: Transaction Inquiry, server to server.
func (c *Client) Inquire(ctx context.Context, transactionID string) (Inquiry, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return Inquiry{}, errors.New("zaincash: inquiry needs a transaction id")
	}

	body, err := c.call(ctx, http.MethodGet, "/api/v2/payment-gateway/transaction/inquiry/"+url.PathEscape(transactionID), nil)
	if err != nil {
		return Inquiry{}, fmt.Errorf("zaincash: inquiry: %w", err)
	}

	return decodeInquiry(body)
}

// decodeInquiry reads the inquiry answer:
//
//	{"status": "OTP_SENT",
//	 "transactionDetails": {"transactionId": "...", "externalReferenceId": "...", ...}, ...}
func decodeInquiry(body []byte) (Inquiry, error) {
	var answer struct {
		Status             string `json:"status"`
		TransactionDetails struct {
			TransactionID       string `json:"transactionId"`
			ExternalReferenceID string `json:"externalReferenceId"`
		} `json:"transactionDetails"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return Inquiry{}, fmt.Errorf("zaincash: decode inquiry response: %w", err)
	}

	if strings.TrimSpace(answer.Status) == "" {
		return Inquiry{}, fmt.Errorf("zaincash: the inquiry response has no status: %s", snippet(body))
	}

	return Inquiry{
		Status:              strings.ToUpper(strings.TrimSpace(answer.Status)),
		TransactionID:       answer.TransactionDetails.TransactionID,
		ExternalReferenceID: answer.TransactionDetails.ExternalReferenceID,
	}, nil
}

// call sends an authorized request and returns the body of a 2xx answer.
func (c *Client) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	accessToken, err := c.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.config.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// The token may have been revoked early: fetch a new one next time.
		c.mu.Lock()
		c.accessToken = ""
		c.mu.Unlock()
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: %s", resp.Status, snippet(respBody))
	}

	return respBody, nil
}

// TokenData is what a verified ZainCash JWT (webhook or redirect callback)
// says about the payment:
//
//	{"eventType": "STATUS_CHANGED", "eventId": "...", "timestamp": "...",
//	 "data": {"transactionId": "...", "merchantReferenceId": "...",
//	          "orderId": "...", "currentStatus": "SUCCESS", "errorMessage": null, ...}}
type TokenData struct {
	EventID             string
	EventType           string
	TransactionID       string
	MerchantReferenceID string
	OrderID             string
	CurrentStatus       string
	PreviousStatus      string
	ErrorMessage        string
}

type tokenClaims struct {
	EventID   string `json:"eventId"`
	EventType string `json:"eventType"`
	Data      struct {
		TransactionID       string  `json:"transactionId"`
		MerchantReferenceID string  `json:"merchantReferenceId"`
		OrderID             string  `json:"orderId"`
		CurrentStatus       string  `json:"currentStatus"`
		PreviousStatus      string  `json:"previousStatus"`
		ErrorMessage        *string `json:"errorMessage"`
	} `json:"data"`

	jwt.RegisteredClaims
}

// VerifyToken checks an HS256 signature against WebhookSecret (the API
// key) and returns what the token says. Used for both the webhook's
// webhook_token and the redirect callback's ?token=.
func (c *Client) VerifyToken(tokenString string) (TokenData, error) {
	if c.config.WebhookSecret == "" {
		return TokenData{}, errors.New("zaincash: ZAINCASH_WEBHOOK_SECRET (the API key) is not set")
	}

	var claims tokenClaims

	parsed, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(token *jwt.Token) (any, error) {
			return []byte(c.config.WebhookSecret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return TokenData{}, fmt.Errorf("zaincash: verify token: %w", err)
	}

	if !parsed.Valid {
		return TokenData{}, errors.New("zaincash: token is not valid")
	}

	data := TokenData{
		EventID:             claims.EventID,
		EventType:           claims.EventType,
		TransactionID:       claims.Data.TransactionID,
		MerchantReferenceID: claims.Data.MerchantReferenceID,
		OrderID:             claims.Data.OrderID,
		CurrentStatus:       strings.ToUpper(strings.TrimSpace(claims.Data.CurrentStatus)),
		PreviousStatus:      claims.Data.PreviousStatus,
	}

	if claims.Data.ErrorMessage != nil {
		data.ErrorMessage = *claims.Data.ErrorMessage
	}

	if data.MerchantReferenceID == "" || data.CurrentStatus == "" {
		return TokenData{}, errors.New("zaincash: the token names no payment (data.merchantReferenceId, data.currentStatus)")
	}

	return data, nil
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

// snippet is a short, single-line piece of an answer for an error message.
func snippet(body []byte) string {
	text := []rune(strings.Join(strings.Fields(string(body)), " "))
	if len(text) > 300 {
		return string(text[:300]) + "…"
	}

	return string(text)
}
