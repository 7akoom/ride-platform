package wallet

import "errors"

var (
	ErrOwnerIDRequired      = errors.New("owner id is required")
	ErrInvalidOwnerType     = errors.New("invalid owner type")
	ErrInvalidPaymentMethod = errors.New("invalid payment method")
	ErrTripIDRequired       = errors.New("trip id is required")
	ErrRiderIDRequired      = errors.New("rider id is required")
	ErrDriverIDRequired     = errors.New("driver id is required")

	ErrInvalidAmount     = errors.New("amount must be a positive decimal value")
	ErrInvalidFareAmount = errors.New("fare amount must be a non-negative decimal value")

	ErrWalletNotFound    = errors.New("wallet not found")
	ErrWalletBlocked     = errors.New("wallet is blocked")
	ErrNoActiveConfig    = errors.New("no active wallet configuration found")
	ErrInsufficientFunds = errors.New("insufficient wallet balance")

	ErrDriverSuspended    = errors.New("driver account is suspended: a commission deposit is required to resume receiving trips")
	ErrBelowMinimumPayout = errors.New("amount is below the minimum payout threshold")

	ErrDuplicateRequest = errors.New("a request with this idempotency key was already processed")
)
