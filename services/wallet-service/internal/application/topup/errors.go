package topup

import "errors"

var (
	ErrOwnerRequired       = errors.New("owner_type and owner_id are required")
	ErrInvalidAmount       = errors.New("amount must be a positive decimal value")
	ErrBelowMinimum        = errors.New("the amount is below the least a top-up may bring")
	ErrAboveMaximum        = errors.New("the amount is above the most a top-up may bring")
	ErrUnknownProvider     = errors.New("this payment provider is not available")
	ErrAmountNotSupported  = errors.New("the payment provider does not take this amount (whole units only)")
	ErrTopUpNotFound       = errors.New("top-up not found")
	ErrInvalidWebhookToken = errors.New("invalid webhook token")
)
