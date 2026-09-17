package topup

import "errors"

var (
	ErrDriverIDRequired    = errors.New("driver id is required")
	ErrInvalidAmount       = errors.New("amount must be a positive decimal value")
	ErrTopUpNotFound       = errors.New("top-up not found")
	ErrInvalidWebhookToken = errors.New("invalid webhook token")
)
