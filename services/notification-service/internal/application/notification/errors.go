package notification

import "errors"

var (
	ErrRecipientIDRequired  = errors.New("recipient id is required")
	ErrInvalidRecipientType = errors.New("invalid recipient type")
	ErrEventKeyRequired     = errors.New("event key is required")
	ErrDeviceTokenRequired  = errors.New("device token is required")
	ErrInvalidPlatform      = errors.New("invalid platform")
	ErrInvalidChannel       = errors.New("invalid channel")

	ErrTemplateNotFound     = errors.New("no template found for this event key")
	ErrTranslationNotFound  = errors.New("template has no translation for any acceptable locale")
	ErrTranslationsRequired = errors.New("at least one translation is required")
)
