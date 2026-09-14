package notification

import (
	"strings"
	"time"
)

type RecipientType string

const (
	RecipientRider  RecipientType = "rider"
	RecipientDriver RecipientType = "driver"
)

func (r RecipientType) Valid() bool {
	return r == RecipientRider || r == RecipientDriver
}

type Channel string

const (
	ChannelInApp Channel = "in_app"
	ChannelPush  Channel = "push"
	ChannelSMS   Channel = "sms"
)

func (c Channel) Valid() bool {
	switch c {
	case ChannelInApp, ChannelPush, ChannelSMS:
		return true
	default:
		return false
	}
}

type DeliveryStatus string

const (
	StatusPending DeliveryStatus = "pending"
	StatusSent    DeliveryStatus = "sent"
	StatusFailed  DeliveryStatus = "failed"
	StatusSkipped DeliveryStatus = "skipped"
)

type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
	PlatformWeb     Platform = "web"
)

func (p Platform) Valid() bool {
	switch p {
	case PlatformAndroid, PlatformIOS, PlatformWeb:
		return true
	default:
		return false
	}
}

// DefaultLocale is the fallback when a recipient has no stored locale
// and the requested one has no translation.
const DefaultLocale = "en"

type Template struct {
	EventKey        string
	DefaultChannels []Channel
}

type TemplateTranslation struct {
	EventKey string
	Locale   string
	Title    string
	Body     string
}

type Device struct {
	ID            string
	RecipientType RecipientType
	RecipientID   string
	DeviceToken   string
	Platform      Platform
	Locale        string
}

type Notification struct {
	ID            string
	RecipientType RecipientType
	RecipientID   string
	EventKey      string
	Title         string
	Body          string
	Locale        string
	Data          map[string]string
	Read          bool
	CreatedAt     time.Time
}

type Delivery struct {
	Channel Channel
	Status  DeliveryStatus
	Detail  string
}

// Render substitutes {placeholders} with the caller's variables.
//
// A variable that has no value is deliberately LEFT AS-IS rather than
// replaced with an empty string: a template referencing {driver_name}
// with no driver name should read obviously broken in testing, not
// quietly ship "  is on the way" to a real user.
func Render(text string, variables map[string]string) string {
	if len(variables) == 0 {
		return text
	}

	replacements := make([]string, 0, len(variables)*2)

	for key, value := range variables {
		replacements = append(replacements, "{"+key+"}", value)
	}

	return strings.NewReplacer(replacements...).Replace(text)
}
