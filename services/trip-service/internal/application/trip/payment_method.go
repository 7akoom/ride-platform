package trip

import (
	"errors"
	"strings"
)

// Payment methods a rider can choose when requesting a trip. They are
// the subset of wallet-service's methods this platform can actually
// collect; adding one means adding a constant here, widening
// trips_payment_method_check in a migration, and teaching wallet-service's
// settlement about it.
const (
	PaymentMethodCash   = "cash"
	PaymentMethodWallet = "wallet"
)

var ErrInvalidPaymentMethod = errors.New("invalid payment method")

// NormalizePaymentMethod lowercases and trims the requested method. Empty
// means the client did not specify one (it predates payment methods) and
// is treated as cash. Anything else, card included, is rejected.
func NormalizePaymentMethod(raw string) (string, error) {
	method := strings.ToLower(strings.TrimSpace(raw))

	switch method {
	case "":
		return PaymentMethodCash, nil
	case PaymentMethodCash, PaymentMethodWallet:
		return method, nil
	default:
		return "", ErrInvalidPaymentMethod
	}
}
