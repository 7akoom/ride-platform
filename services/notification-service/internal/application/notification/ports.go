package notification

import "context"

type PersistInput struct {
	RecipientType  RecipientType
	RecipientID    string
	EventKey       string
	Title          string
	Body           string
	Locale         string
	Data           map[string]string
	IdempotencyKey string
	Deliveries     []Delivery
}

type UpsertTemplateInput struct {
	EventKey        string
	DefaultChannels []Channel
	Translations    []TemplateTranslation
}

type RegisterDeviceInput struct {
	RecipientType RecipientType
	RecipientID   string
	DeviceToken   string
	Platform      Platform
	Locale        string
}

type ListInput struct {
	RecipientType RecipientType
	RecipientID   string
	Limit         int
	UnreadOnly    bool
}

type Repository interface {
	FindTemplate(ctx context.Context, eventKey string) (Template, error)

	// FindTranslation tries each locale in order and returns the first
	// that exists, so the caller can express a preference chain
	// (requested -> recipient's -> default) in one round trip.
	FindTranslation(ctx context.Context, eventKey string, locales []string) (TemplateTranslation, error)

	UpsertTemplate(ctx context.Context, input UpsertTemplateInput) (int, error)

	RegisterDevice(ctx context.Context, input RegisterDeviceInput) (string, error)
	UnregisterDevice(ctx context.Context, deviceToken string) (bool, error)
	ListDevices(ctx context.Context, recipientType RecipientType, recipientID string) ([]Device, error)

	// RemoveDeviceTokens drops tokens the push provider reported as
	// permanently invalid (app uninstalled, token rotated). Without
	// this, dead tokens accumulate forever and every send wastes calls
	// on them.
	RemoveDeviceTokens(ctx context.Context, tokens []string) error

	FindNotificationByIdempotencyKey(ctx context.Context, key string) (Notification, []Delivery, bool, error)
	Persist(ctx context.Context, input PersistInput) (Notification, error)

	List(ctx context.Context, input ListInput) ([]Notification, error)
	UnreadCount(ctx context.Context, recipientType RecipientType, recipientID string) (int, error)
	MarkAsRead(ctx context.Context, recipientType RecipientType, recipientID string, ids []string) (int, error)
}

// PushResult reports which tokens failed permanently, so the caller can
// prune them.
type PushResult struct {
	SentCount     int
	InvalidTokens []string
	Detail        string
}

// PushSender is the port for whatever actually delivers push
// notifications. There is no self-hostable alternative here — reaching
// an Android or iOS device means going through FCM or APNs — so the
// design keeps that dependency behind an interface: a deployment swaps
// the implementation without the rest of the service knowing.
type PushSender interface {
	Send(ctx context.Context, devices []Device, title, body string, data map[string]string) (PushResult, error)
}

// SMSSender is the port for SMS delivery. Providers are strictly
// regional (an Iraqi deployment and a Gulf one will use different
// gateways), so this stays an interface with a no-op default rather
// than baking in a provider nobody can use everywhere.
type SMSSender interface {
	Send(ctx context.Context, recipientType RecipientType, recipientID, body string) error
}
