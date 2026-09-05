package otp

import "errors"

type ProviderFailoverPolicy interface {
	ShouldFailover(
		err error,
	) bool
}

type ConservativeProviderFailoverPolicy struct{}

func (ConservativeProviderFailoverPolicy) ShouldFailover(
	err error,
) bool {
	if err == nil {
		return false
	}

	var providerErr *SMSProviderError

	if !errors.As(
		err,
		&providerErr,
	) {
		return false
	}

	switch providerErr.Kind {
	case SMSProviderErrorRateLimited:
		return true

	case SMSProviderErrorPermanent,
		SMSProviderErrorTemporary,
		SMSProviderErrorUnknownDeliveryState:
		return false

	default:
		return false
	}
}
