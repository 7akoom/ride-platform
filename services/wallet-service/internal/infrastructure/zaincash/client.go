// Package zaincash is a thin client for the ZainCash Payment Gateway v2
// (https://docs.zaincash.iq): OAuth2 client-credentials auth, creating a
// payment session, and verifying the HS256 JWT ZainCash sends back on
// both the redirect callback and the webhook. It knows nothing about
// wallets or drivers — that orchestration lives in
// internal/application/topup.
package zaincash

import (
	"bytes"
	"context"
	"encoding/json"
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
	BaseURL      string
	ClientID     string
	ClientSecret string
	// Space-separated, e.g. "payment:read payment:write".
	Scope string
	// Verifies the HS256 JWT on both the redirect callback and the
	// webhook. Per ZainCash's docs this is their "API Secret Key" —
	// confirm with ZainCash's business team at onboarding whether it's
	// the same value as ClientSecret or a separate one; this is kept
	// as its own field so the two can differ.
	WebhookSecret string
}

type Client struct {
	httpClient *http.Client
	config     Config

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func NewClient(config Config) *Client {
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
		return "", fmt.Errorf("zaincash: oauth2 token request failed: %s: %s", resp.Status, body)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("zaincash: decode oauth2 token response: %w", err)
	}

	c.accessToken = tokenResponse.AccessToken
	// Refresh 30s early so a request in flight never races the actual
	// expiry.
	c.expiresAt = time.Now().Add(time.Duration(tokenResponse.ExpiresIn)*time.Second - 30*time.Second)

	return c.accessToken, nil
}

type InitTransactionInput struct {
	// "en", "ar" or "ku".
	Language string

	// Our own idempotency key — becomes ZainCash's externalReferenceId.
	ExternalReferenceID string

	OrderID     string
	ServiceType string

	// Whole IQD amount as a decimal string (ZainCash rejects decimals
	// on some endpoints; keep top-ups whole-unit).
	AmountValue string

	// Optional. Omit on a driver's first top-up; ZainCash then prompts
	// for the wallet number itself and we learn it from the redirect
	// callback (see docs' "Customer Wallet Management" section) — not
	// implemented yet, since there's no app UI to capture it into yet.
	CustomerPhone string

	SuccessURL string
	FailureURL string
}

type InitTransactionResult struct {
	TransactionID string
	RedirectURL   string
}

func (c *Client) InitTransaction(
	ctx context.Context,
	input InitTransactionInput,
) (InitTransactionResult, error) {
	accessToken, err := c.token(ctx)
	if err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: get access token: %w", err)
	}

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

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.config.BaseURL+"/api/v2/payment-gateway/transaction/init",
		bytes.NewReader(body),
	)
	if err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: build init request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: init request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: read init response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return InitTransactionResult{}, fmt.Errorf("zaincash: init failed: %s: %s", resp.Status, respBody)
	}

	var initResponse struct {
		TransactionID string `json:"transactionId"`
		RedirectURL   string `json:"redirectUrl"`
	}
	if err := json.Unmarshal(respBody, &initResponse); err != nil {
		return InitTransactionResult{}, fmt.Errorf("zaincash: decode init response: %w", err)
	}

	return InitTransactionResult{
		TransactionID: initResponse.TransactionID,
		RedirectURL:   initResponse.RedirectURL,
	}, nil
}

// WebhookClaims is the subset of a verified ZainCash JWT (webhook or
// redirect callback) this service actually needs. Field names for
// transactionId/merchantReferenceId/orderId/currentStatus/
// previousStatus/eventId/eventType come straight from ZainCash's docs
// prose; the docs' own rendered JSON examples weren't machine-readable
// when this was written, so treat these as best-effort until confirmed
// against a real captured payload.
type WebhookClaims struct {
	EventID             string `json:"eventId"`
	EventType           string `json:"eventType"`
	TransactionID       string `json:"transactionId"`
	MerchantReferenceID string `json:"merchantReferenceId"`
	OrderID             string `json:"orderId"`
	CurrentStatus       string `json:"currentStatus"`
	PreviousStatus      string `json:"previousStatus"`

	jwt.RegisteredClaims
}

// VerifyToken checks an HS256 signature against WebhookSecret and
// returns the decoded claims. Used for both the webhook's
// webhook_token and the redirect callback's ?token=.
func (c *Client) VerifyToken(tokenString string) (WebhookClaims, error) {
	var claims WebhookClaims

	parsed, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			return []byte(c.config.WebhookSecret), nil
		},
	)
	if err != nil {
		return WebhookClaims{}, fmt.Errorf("zaincash: verify token: %w", err)
	}

	if !parsed.Valid {
		return WebhookClaims{}, fmt.Errorf("zaincash: token is not valid")
	}

	return claims, nil
}
