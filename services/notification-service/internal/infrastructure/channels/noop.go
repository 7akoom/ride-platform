package channels

import (
	"context"
	"errors"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// NoopPushSender stands in when push isn't configured for a deployment.
// It reports SKIPPED rather than pretending to succeed — a delivery row
// that says "not configured" is honest; one that says "sent" when
// nothing was sent is a lie that costs hours to debug later.
type NoopPushSender struct{}

func NewNoopPushSender() *NoopPushSender {
	return &NoopPushSender{}
}

func (s *NoopPushSender) Send(
	ctx context.Context,
	devices []notification.Device,
	title, body string,
	data map[string]string,
) (notification.PushResult, error) {
	return notification.PushResult{}, errors.New("push is not configured for this deployment")
}

// NoopSMSSender stands in when no SMS gateway is configured. SMS
// providers are strictly regional, so most deployments will start here
// and wire a local gateway later.
type NoopSMSSender struct{}

func NewNoopSMSSender() *NoopSMSSender {
	return &NoopSMSSender{}
}

func (s *NoopSMSSender) Send(
	ctx context.Context,
	recipientType notification.RecipientType,
	recipientID, body string,
) error {
	return errors.New("SMS is not configured for this deployment")
}
