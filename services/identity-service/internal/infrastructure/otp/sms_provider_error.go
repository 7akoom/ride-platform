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

	if e.StatusCode > 0 {
		return fmt.Sprintf(
			"SMS provider %s failed with %s error and HTTP status %d",
			e.Provider,
			e.Kind,
			e.StatusCode,
		)
	}

	return fmt.Sprintf(
		"SMS provider %s failed with %s error",
		e.Provider,
		e.Kind,
	)
}

func (e *SMSProviderError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}
