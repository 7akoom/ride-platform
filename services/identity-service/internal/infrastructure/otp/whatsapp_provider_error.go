package otp

import (
	"fmt"
)

type WhatsAppProviderErrorKind string

const (
	WhatsAppProviderErrorRateLimited WhatsAppProviderErrorKind = "rate_limited"

	WhatsAppProviderErrorTemporary WhatsAppProviderErrorKind = "temporary"

	WhatsAppProviderErrorPermanent WhatsAppProviderErrorKind = "permanent"

	WhatsAppProviderErrorUnknownDeliveryState WhatsAppProviderErrorKind = "unknown_delivery_state"
)

type WhatsAppProviderError struct {
	Provider   string
	Kind       WhatsAppProviderErrorKind
	StatusCode int
	Err        error
}

func (e *WhatsAppProviderError) Error() string {
	if e == nil {
		return "WhatsApp provider error"
	}

	if e.StatusCode > 0 {
		return fmt.Sprintf(
			"WhatsApp provider %s failed with %s error and HTTP status %d",
			e.Provider,
			e.Kind,
			e.StatusCode,
		)
	}

	return fmt.Sprintf(
		"WhatsApp provider %s failed with %s error",
		e.Provider,
		e.Kind,
	)
}

func (e *WhatsAppProviderError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}
