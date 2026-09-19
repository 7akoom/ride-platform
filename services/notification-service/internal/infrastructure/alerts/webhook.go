package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
)

// SignatureHeader carries "sha256=<hex>", the HMAC-SHA256 of the request
// body under the shared secret, when a secret is configured.
const SignatureHeader = "X-Signature-256"

// Webhook posts every alert as JSON to one URL, so a deployment can plug
// in any chat or paging system without code changes here.
type Webhook struct {
	client HTTPDoer
	url    string
	secret string
}

type webhookBody struct {
	Event          string   `json:"event"`
	AlertID        string   `json:"alert_id"`
	TripID         string   `json:"trip_id"`
	TriggeredBy    string   `json:"triggered_by"`
	DriverName     string   `json:"driver_name,omitempty"`
	Latitude       *float64 `json:"latitude,omitempty"`
	Longitude      *float64 `json:"longitude,omitempty"`
	LocationSource string   `json:"location_source,omitempty"`
	MapURL         string   `json:"map_url,omitempty"`
	TriggeredAt    string   `json:"triggered_at,omitempty"`
	Text           string   `json:"text"`
}

func NewWebhook(client HTTPDoer, url string, secret string) (*Webhook, error) {
	switch {
	case client == nil:
		return nil, errors.New("webhook alerter needs an HTTP client")
	case strings.TrimSpace(url) == "":
		return nil, errors.New("webhook alerter needs a URL")
	}

	return &Webhook{client: client, url: url, secret: secret}, nil
}

func (w *Webhook) Alert(ctx context.Context, alert events.SOSAlert) error {
	body := webhookBody{
		Event:       "sos_alert",
		AlertID:     alert.AlertID,
		TripID:      alert.TripID,
		TriggeredBy: alert.TriggeredBy,
		DriverName:  alert.DriverName,
		Text:        alert.Text(),
	}

	if alert.HasLocation {
		latitude, longitude := alert.Latitude, alert.Longitude
		body.Latitude = &latitude
		body.Longitude = &longitude
		body.LocationSource = alert.LocationSource
		body.MapURL = alert.MapURL()
	}

	if !alert.TriggeredAt.IsZero() {
		body.TriggeredAt = alert.TriggeredAt.UTC().Format(time.RFC3339)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode webhook body: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	if w.secret != "" {
		mac := hmac.New(sha256.New, []byte(w.secret))
		mac.Write(payload)
		request.Header.Set(SignatureHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	response, err := w.client.Do(request)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}

	if response == nil {
		return errors.New("post webhook: no response")
	}

	if response.Body != nil {
		defer response.Body.Close()
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("post webhook: endpoint answered HTTP %d", response.StatusCode)
	}

	return nil
}
