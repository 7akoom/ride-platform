package alerts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/notification-service/internal/config"
)

var testAlert = events.SOSAlert{
	AlertID:        "3f2a91bc-1111-2222-3333-444455556666",
	TripID:         "trip-1",
	TriggeredBy:    "rider",
	DriverName:     "Test Driver",
	HasLocation:    true,
	Latitude:       36.19,
	Longitude:      44.01,
	LocationSource: events.LocationSourceSOSPress,
	TriggeredAt:    time.Date(2026, 9, 19, 13, 8, 0, 0, time.UTC),
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type recordedRequest struct {
	header http.Header
	body   []byte
}

// gateway is a fake HTTP endpoint that records what it receives and
// answers with a configurable status per call.
type gateway struct {
	mu       sync.Mutex
	requests []recordedRequest
	statuses []int
}

func (g *gateway) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		g.mu.Lock()
		defer g.mu.Unlock()

		g.requests = append(g.requests, recordedRequest{header: r.Header.Clone(), body: body})

		status := http.StatusOK
		if len(g.statuses) >= len(g.requests) {
			status = g.statuses[len(g.requests)-1]
		}

		w.WriteHeader(status)
	})
}

func newGateway(statuses ...int) (*gateway, *httptest.Server) {
	g := &gateway{statuses: statuses}

	return g, httptest.NewServer(g.handler())
}

func TestSMSSendsTheAlertToEveryOperatorPhone(t *testing.T) {
	g, server := newGateway()
	defer server.Close()

	sms, err := NewSMS(server.Client(), server.URL, "secret-key", "Ride", []string{"9647701234567", "9647809876543"}, discardLogger())
	if err != nil {
		t.Fatalf("NewSMS: %v", err)
	}

	if err := sms.Alert(context.Background(), testAlert); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if len(g.requests) != 2 {
		t.Fatalf("expected one SMS per phone, got %d", len(g.requests))
	}

	for i, want := range []string{"9647701234567", "9647809876543"} {
		request := g.requests[i]

		if got := request.header.Get("Authorization"); got != "Bearer secret-key" {
			t.Fatalf("unexpected Authorization header %q", got)
		}

		var body map[string]string
		if err := json.Unmarshal(request.body, &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}

		if body["recipient"] != want || body["sender_id"] != "Ride" || body["type"] != "plain" {
			t.Fatalf("unexpected SMS request: %v", body)
		}

		if !strings.Contains(body["message"], "SOS ALERT") || !strings.Contains(body["message"], "trip-1") {
			t.Fatalf("the SMS text is missing the alert: %q", body["message"])
		}
	}
}

func TestSMSSucceedsWhenAtLeastOnePhoneWasReached(t *testing.T) {
	_, server := newGateway(http.StatusBadRequest, http.StatusOK)
	defer server.Close()

	sms, _ := NewSMS(server.Client(), server.URL, "k", "Ride", []string{"9647701234567", "9647809876543"}, discardLogger())

	if err := sms.Alert(context.Background(), testAlert); err != nil {
		t.Fatalf("one reachable operator is enough, got %v", err)
	}
}

func TestSMSFailsOnlyWhenNoPhoneWasReached(t *testing.T) {
	_, server := newGateway(http.StatusServiceUnavailable, http.StatusTooManyRequests)
	defer server.Close()

	sms, _ := NewSMS(server.Client(), server.URL, "k", "Ride", []string{"9647701234567", "9647809876543"}, discardLogger())

	err := sms.Alert(context.Background(), testAlert)
	if err == nil {
		t.Fatalf("expected an error when nobody was reached")
	}

	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "36.19") {
		t.Fatalf("errors must not leak the key or the location: %v", err)
	}
}

func TestNewSMSRejectsIncompleteConfiguration(t *testing.T) {
	client := &http.Client{}
	phones := []string{"9647701234567"}

	cases := map[string]func() (*SMS, error){
		"no client":   func() (*SMS, error) { return NewSMS(nil, "http://x", "k", "s", phones, discardLogger()) },
		"no endpoint": func() (*SMS, error) { return NewSMS(client, "", "k", "s", phones, discardLogger()) },
		"no key":      func() (*SMS, error) { return NewSMS(client, "http://x", "", "s", phones, discardLogger()) },
		"no sender":   func() (*SMS, error) { return NewSMS(client, "http://x", "k", "", phones, discardLogger()) },
		"no phones":   func() (*SMS, error) { return NewSMS(client, "http://x", "k", "s", nil, discardLogger()) },
		"no logger":   func() (*SMS, error) { return NewSMS(client, "http://x", "k", "s", phones, nil) },
	}

	for name, build := range cases {
		if _, err := build(); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestWebhookPostsTheAlertAsSignedJSON(t *testing.T) {
	g, server := newGateway()
	defer server.Close()

	webhook, err := NewWebhook(server.Client(), server.URL, "shared-secret")
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}

	if err := webhook.Alert(context.Background(), testAlert); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	request := g.requests[0]

	mac := hmac.New(sha256.New, []byte("shared-secret"))
	mac.Write(request.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if got := request.header.Get(SignatureHeader); got != want {
		t.Fatalf("signature %q does not match the body, want %q", got, want)
	}

	var body map[string]any
	if err := json.Unmarshal(request.body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	if body["event"] != "sos_alert" || body["trip_id"] != "trip-1" || body["triggered_by"] != "rider" {
		t.Fatalf("unexpected body: %v", body)
	}

	if body["latitude"] != 36.19 || body["location_source"] != events.LocationSourceSOSPress {
		t.Fatalf("location missing from the body: %v", body)
	}

	if !strings.Contains(body["map_url"].(string), "openstreetmap.org") {
		t.Fatalf("map_url should be an OpenStreetMap link: %v", body["map_url"])
	}
}

func TestWebhookWithoutASecretSendsNoSignature(t *testing.T) {
	g, server := newGateway()
	defer server.Close()

	webhook, _ := NewWebhook(server.Client(), server.URL, "")

	if err := webhook.Alert(context.Background(), testAlert); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got := g.requests[0].header.Get(SignatureHeader); got != "" {
		t.Fatalf("no secret configured, yet a signature was sent: %q", got)
	}
}

func TestWebhookOmitsTheLocationWhenThereIsNone(t *testing.T) {
	g, server := newGateway()
	defer server.Close()

	webhook, _ := NewWebhook(server.Client(), server.URL, "")

	alert := testAlert
	alert.HasLocation = false

	if err := webhook.Alert(context.Background(), alert); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	var body map[string]any
	_ = json.Unmarshal(g.requests[0].body, &body)

	if _, present := body["latitude"]; present {
		t.Fatalf("a missing location must not be reported as 0,0: %v", body)
	}
}

func TestWebhookReportsANon2xxAnswerAsAFailure(t *testing.T) {
	_, server := newGateway(http.StatusInternalServerError)
	defer server.Close()

	webhook, _ := NewWebhook(server.Client(), server.URL, "")

	if err := webhook.Alert(context.Background(), testAlert); err == nil {
		t.Fatalf("expected an error for HTTP 500")
	}
}

func TestMultiCountsAsDeliveredWhenAnyChannelGotThrough(t *testing.T) {
	_, smsServer := newGateway(http.StatusOK)
	defer smsServer.Close()

	_, webhookServer := newGateway(http.StatusInternalServerError)
	defer webhookServer.Close()

	sms, _ := NewSMS(smsServer.Client(), smsServer.URL, "k", "Ride", []string{"9647701234567"}, discardLogger())
	webhook, _ := NewWebhook(webhookServer.Client(), webhookServer.URL, "")

	multi := &Multi{
		logger:   discardLogger(),
		channels: []channel{{name: "sms", alerter: sms}, {name: "webhook", alerter: webhook}},
	}

	if err := multi.Alert(context.Background(), testAlert); err != nil {
		t.Fatalf("a dead webhook must not fail a delivered SMS, got %v", err)
	}
}

func TestMultiFailsOnlyWhenEveryChannelFailed(t *testing.T) {
	_, smsServer := newGateway(http.StatusBadGateway)
	defer smsServer.Close()

	_, webhookServer := newGateway(http.StatusInternalServerError)
	defer webhookServer.Close()

	sms, _ := NewSMS(smsServer.Client(), smsServer.URL, "k", "Ride", []string{"9647701234567"}, discardLogger())
	webhook, _ := NewWebhook(webhookServer.Client(), webhookServer.URL, "")

	multi := &Multi{
		logger:   discardLogger(),
		channels: []channel{{name: "sms", alerter: sms}, {name: "webhook", alerter: webhook}},
	}

	err := multi.Alert(context.Background(), testAlert)
	if err == nil {
		t.Fatalf("expected an error when every channel failed")
	}

	if !strings.Contains(err.Error(), "sms") || !strings.Contains(err.Error(), "webhook") {
		t.Fatalf("the error should name both failed channels: %v", err)
	}
}

func TestNewReturnsNilWhenNothingIsConfigured(t *testing.T) {
	alerter, err := New(config.SOSAlerts{}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if alerter != nil {
		t.Fatalf("nothing configured must yield a nil alerter so the handler can say so loudly")
	}
}

func TestNewBuildsBothChannelsFromConfiguration(t *testing.T) {
	alerter, err := New(config.SOSAlerts{
		OperatorPhones: []string{"9647701234567"},
		SMSEndpoint:    "https://sms.example/send",
		SMSAPIKey:      "k",
		SMSSenderID:    "Ride",
		WebhookURL:     "https://hooks.example/sos",
		Timeout:        5 * time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	multi, ok := alerter.(*Multi)
	if !ok || len(multi.channels) != 2 {
		t.Fatalf("expected an sms and a webhook channel, got %#v", alerter)
	}
}
