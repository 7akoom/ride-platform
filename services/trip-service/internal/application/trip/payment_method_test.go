package trip

import (
	"errors"
	"testing"
)

func TestNormalizePaymentMethod(t *testing.T) {
	valid := map[string]string{
		"":         PaymentMethodCash,
		"   ":      PaymentMethodCash,
		"cash":     PaymentMethodCash,
		" CASH ":   PaymentMethodCash,
		"wallet":   PaymentMethodWallet,
		"Wallet":   PaymentMethodWallet,
		"\twallet": PaymentMethodWallet,
	}

	for input, want := range valid {
		got, err := NormalizePaymentMethod(input)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", input, err)
		}

		if got != want {
			t.Fatalf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestNormalizePaymentMethodRejectsUnsupportedMethods(t *testing.T) {
	// card has no processor behind it yet, so it must be refused up front
	// rather than accepted and then failing at settlement.
	for _, input := range []string{"card", "CARD", "bitcoin", "cash,wallet", "0"} {
		if _, err := NormalizePaymentMethod(input); !errors.Is(err, ErrInvalidPaymentMethod) {
			t.Fatalf("%q: expected ErrInvalidPaymentMethod, got %v", input, err)
		}
	}
}
