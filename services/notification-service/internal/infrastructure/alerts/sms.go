package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
)

// HTTPDoer is the slice of *http.Client the alerters need, so tests can
// substitute a fake.
type HTTPDoer interface {
	Do(request *http.Request) (*http.Response, error)
}

// SMS alerts a fixed list of operator phones through the BulkSMSIraq
// gateway, using the same plain-SMS request identity-service uses for its
// own messages.
type SMS struct {
	client   HTTPDoer
	endpoint string
	apiKey   string
	senderID string
	phones   []string
	logger   *slog.Logger
}

type smsRequest struct {
	Recipient string `json:"recipient"`
	SenderID  string `json:"sender_id"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

func NewSMS(
	client HTTPDoer,
	endpoint string,
	apiKey string,
	senderID string,
	phones []string,
	logger *slog.Logger,
) (*SMS, error) {
	switch {
	case client == nil:
		return nil, errors.New("SMS alerter needs an HTTP client")
	case strings.TrimSpace(endpoint) == "":
		return nil, errors.New("SMS alerter needs an endpoint")
	case strings.TrimSpace(apiKey) == "":
		return nil, errors.New("SMS alerter needs an API key")
	case strings.TrimSpace(senderID) == "":
		return nil, errors.New("SMS alerter needs a sender id")
	case len(phones) == 0:
		return nil, errors.New("SMS alerter needs at least one phone")
	case logger == nil:
		return nil, errors.New("SMS alerter needs a logger")
	}

	return &SMS{
		client:   client,
		endpoint: endpoint,
		apiKey:   apiKey,
		senderID: senderID,
		phones:   phones,
		logger:   logger,
	}, nil
}

// Alert texts every operator. It succeeds if at least one of them was
// reached: one bad number must not make the others get paged twice on a
// retry. Only when nobody could be reached does it return an error.
func (s *SMS) Alert(ctx context.Context, alert events.SOSAlert) error {
	text := alert.Text()

	var failures []error

	reached := 0

	for _, phone := range s.phones {
		if err := s.send(ctx, phone, text); err != nil {
			s.logger.WarnContext(ctx, "SMS to an SOS operator phone failed",
				"alert_id", alert.AlertID,
				"error", err,
			)

			failures = append(failures, err)

			continue
		}

		reached++
	}

	if reached == 0 {
		return fmt.Errorf("no operator phone could be reached: %w", errors.Join(failures...))
	}

	return nil
}

func (s *SMS) send(ctx context.Context, phone string, text string) error {
	payload, err := json.Marshal(smsRequest{
		Recipient: phone,
		SenderID:  s.senderID,
		Type:      "plain",
		Message:   text,
	})
	if err != nil {
		return fmt.Errorf("encode SMS request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create SMS request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+s.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("send SMS: %w", err)
	}

	if response == nil {
		return errors.New("send SMS: no response")
	}

	if response.Body != nil {
		defer response.Body.Close()
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// Status only: the body may echo the request, and the request
		// carries a person's location.
		return fmt.Errorf("send SMS: gateway answered HTTP %d", response.StatusCode)
	}

	return nil
}
