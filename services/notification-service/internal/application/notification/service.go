package notification

import "context"

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
}

type Service interface {
	Send(ctx context.Context, input SendInput) (SendResult, error)
	RegisterDevice(ctx context.Context, input RegisterDeviceInput) (string, error)
	UnregisterDevice(ctx context.Context, deviceToken string) (bool, error)
	List(ctx context.Context, input ListInput) (ListResult, error)
	MarkAsRead(ctx context.Context, recipientType RecipientType, recipientID string, ids []string) (int, error)
	UpsertTemplate(ctx context.Context, input UpsertTemplateInput) (int, error)
}

type service struct {
	repository Repository
	pushSender PushSender
	smsSender  SMSSender
}

func NewService(
	repository Repository,
	pushSender PushSender,
	smsSender SMSSender,
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

	return &service{
		repository: repository,
		pushSender: pushSender,
		smsSender:  smsSender,
	}
}
