package otp

import (
	"fmt"
)

type SMSProviderErrorKind string

const (
	SMSProviderErrorRateLimited SMSProviderErrorKind = "rate_limited"

	SMSProviderErrorTemporary SMSProviderErrorKind = "temporary"

	SMSProviderErrorPermanent SMSProviderErrorKind = "permanent"

	SMSProviderErrorUnknownDeliveryState SMSProviderErrorKind = "unknown_delivery_state"
)

type SMSProviderError struct {
	Provider   string
	Kind       SMSProviderErrorKind
	StatusCode int
	Err        error
}

func (e *SMSProviderError) Error() string {
	if e == nil {
		return "SMS provider error"
	}

	text := fmt.Sprintf(
		"SMS provider %s failed with %s error",
		e.Provider,
		e.Kind,
	)

	if e.StatusCode > 0 {
		text += fmt.Sprintf(" and HTTP status %d", e.StatusCode)
	}

	// The provider's own reason (a wrong key, no balance), so the log says
	// what to fix.
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}

	return text
}

func (e *SMSProviderError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}
