package notification

import (
	"context"
	"strings"
)

type SendInput struct {
	RecipientType  RecipientType
	RecipientID    string
	EventKey       string
	Variables      map[string]string
	Data           map[string]string
	Locale         string
	Channels       []Channel
	IdempotencyKey string
}

type SendResult struct {
	Notification Notification
	Deliveries   []Delivery
}

type ListResult struct {
	Notifications []Notification
	UnreadCount   int
	NextPageToken string
}

type Service interface {
	Send(ctx context.Context, input SendInput) (SendResult, error)
	RegisterDevice(ctx context.Context, input RegisterDeviceInput) (string, error)
	UnregisterDevice(ctx context.Context, deviceToken string) (bool, error)
	List(ctx context.Context, input ListInput) (ListResult, error)
	MarkAsRead(ctx context.Context, recipientType RecipientType, recipientID string, ids []string) (int, error)
	UnreadCount(ctx context.Context, recipientType RecipientType, recipientID string) (int, error)
	UpsertTemplate(ctx context.Context, input UpsertTemplateInput) (int, error)
}

type service struct {
	repository Repository
	pushSender PushSender
	smsSender  SMSSender

	// defaultLocale is the language of a recipient with no device that says
	// one (see WithDefaultLocale).
	defaultLocale string
}

// Option customises the service at construction time.
type Option func(*service)

// WithDefaultLocale sets the language notifications are written in for a
// recipient whose devices do not say one (DefaultLocale when empty). An instance
// serves one country, so this is its language.
func WithDefaultLocale(locale string) Option {
	return func(s *service) {
		if locale = strings.TrimSpace(locale); locale != "" {
			s.defaultLocale = locale
		}
	}
}

func NewService(
	repository Repository,
	pushSender PushSender,
	smsSender SMSSender,
	options ...Option,
) Service {
	if repository == nil {
		panic("notification repository is required")
	}

	if pushSender == nil {
		panic("push sender is required (use the no-op sender if push is not configured)")
	}

	if smsSender == nil {
		panic("sms sender is required (use the no-op sender if SMS is not configured)")
	}

	s := &service{
		repository:    repository,
		pushSender:    pushSender,
		smsSender:     smsSender,
		defaultLocale: DefaultLocale,
	}

	for _, option := range options {
		option(s)
	}

	return s
}
