package otp

import "fmt"

type EmailProviderErrorKind string

const (
	EmailProviderErrorRateLimited EmailProviderErrorKind = "rate_limited"

	EmailProviderErrorTemporary EmailProviderErrorKind = "temporary"

	EmailProviderErrorPermanent EmailProviderErrorKind = "permanent"

	EmailProviderErrorUnknownDeliveryState EmailProviderErrorKind = "unknown_delivery_state"
)

type EmailProviderError struct {
	Provider   string
	Kind       EmailProviderErrorKind
	StatusCode int
	Err        error
}

func (e *EmailProviderError) Error() string {
	if e == nil {
		return "email provider error"
	}

	if e.StatusCode > 0 {
		return fmt.Sprintf(
			"email provider %s failed with %s error and HTTP status %d",
			e.Provider,
			e.Kind,
			e.StatusCode,
		)
	}

	return fmt.Sprintf(
		"email provider %s failed with %s error",
		e.Provider,
		e.Kind,
	)
}

func (e *EmailProviderError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}
