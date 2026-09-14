package channels

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// FCM is free with no per-message cost, and it is the only route to an
// Android device — Google controls the channel. This client talks to the
// HTTP v1 API directly rather than pulling in the Firebase Admin SDK,
// which drags in a large dependency tree for what amounts to one signed
// JWT and one POST per device.
//
// Credentials come from a Firebase service-account JSON key. Generate
// one at: Firebase console -> Project settings -> Service accounts ->
// Generate new private key. Point FCM_CREDENTIALS_FILE at it.
const (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	fcmScope       = "https://www.googleapis.com/auth/firebase.messaging"
	fcmSendURLTmpl = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
)

// ServiceAccount is the subset of the Firebase key file this needs.
type ServiceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

type FCMSender struct {
	account    ServiceAccount
	httpClient *http.Client
	privateKey *rsa.PrivateKey

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewFCMSender(account ServiceAccount, timeout time.Duration) (*FCMSender, error) {
	if account.ProjectID == "" || account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, fmt.Errorf("service account is missing project_id, client_email, or private_key")
	}

	privateKey, err := parsePrivateKey(account.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse service account private key: %w", err)
	}

	return &FCMSender{
		account:    account,
		privateKey: privateKey,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func LoadServiceAccount(raw []byte) (ServiceAccount, error) {
	var account ServiceAccount

	if err := json.Unmarshal(raw, &account); err != nil {
		return ServiceAccount{}, fmt.Errorf("decode service account JSON: %w", err)
	}

	return account, nil
}

// Send posts one message per device. FCM's v1 API has no true multicast
// endpoint, so this loops — fine at the volumes a single deployment
// sees, since a recipient has a handful of devices at most.
func (s *FCMSender) Send(
	ctx context.Context,
	devices []notification.Device,
	title, body string,
	data map[string]string,
) (notification.PushResult, error) {
	accessToken, err := s.ensureAccessToken(ctx)
	if err != nil {
		return notification.PushResult{}, fmt.Errorf("obtain FCM access token: %w", err)
	}

	endpoint := fmt.Sprintf(fcmSendURLTmpl, s.account.ProjectID)

	result := notification.PushResult{}

	var failures []string

	for _, device := range devices {
		status, err := s.sendOne(ctx, endpoint, accessToken, device.DeviceToken, title, body, data)

		switch {
		case err != nil:
			failures = append(failures, err.Error())

		case status == http.StatusOK:
			result.SentCount++

		// 404 UNREGISTERED and 400 INVALID_ARGUMENT on a token mean the
		// token is permanently dead — the app was uninstalled or the
		// token rotated. Collecting these lets the caller prune them so
		// they stop being retried forever.
		case status == http.StatusNotFound || status == http.StatusBadRequest:
			result.InvalidTokens = append(result.InvalidTokens, device.DeviceToken)

		default:
			failures = append(failures, fmt.Sprintf("FCM returned status %d", status))
		}
	}

	if len(result.InvalidTokens) > 0 {
		failures = append(failures, fmt.Sprintf("%d invalid token(s) pruned", len(result.InvalidTokens)))
	}

	result.Detail = strings.Join(failures, "; ")

	return result, nil
}

type fcmMessage struct {
	Message struct {
		Token        string            `json:"token"`
		Notification fcmNotification   `json:"notification"`
		Data         map[string]string `json:"data,omitempty"`
	} `json:"message"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *FCMSender) sendOne(
	ctx context.Context,
	endpoint, accessToken, deviceToken, title, body string,
	data map[string]string,
) (int, error) {
	var message fcmMessage
	message.Message.Token = deviceToken
	message.Message.Notification = fcmNotification{Title: title, Body: body}
	message.Message.Data = data

	payload, err := json.Marshal(message)
	if err != nil {
		return 0, fmt.Errorf("marshal FCM message: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("build FCM request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := s.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("call FCM: %w", err)
	}
	defer response.Body.Close()

	return response.StatusCode, nil
}

// ensureAccessToken caches the OAuth token until shortly before it
// expires. Without caching, every notification would cost an extra
// round trip to Google just to authenticate.
func (s *FCMSender) ensureAccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && time.Now().Before(s.tokenExpiry) {
		return s.accessToken, nil
	}

	assertion, err := s.buildJWT()
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		googleTokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := s.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request access token: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", response.StatusCode)
	}

	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	s.accessToken = decoded.AccessToken
	// Refresh a minute early so a token never expires mid-flight.
	s.tokenExpiry = time.Now().Add(time.Duration(decoded.ExpiresIn-60) * time.Second)

	return s.accessToken, nil
}

func (s *FCMSender) buildJWT() (string, error) {
	now := time.Now()

	header := base64URL([]byte(`{"alg":"RS256","typ":"JWT"}`))

	claims, err := json.Marshal(map[string]any{
		"iss":   s.account.ClientEmail,
		"scope": fcmScope,
		"aud":   googleTokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal JWT claims: %w", err)
	}

	signingInput := header + "." + base64URL(claims)

	hashed := crypto.SHA256.New()
	hashed.Write([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, hashed.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signingInput + "." + base64URL(signature), nil
}

func base64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key")
	}

	// Firebase keys are PKCS#8; older keys may be PKCS#1.
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}

		return rsaKey, nil
	}

	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
