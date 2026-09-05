package otp

import (
	"errors"
	"testing"
)

func TestConservativeProviderFailoverPolicyAllowsRateLimited(
	t *testing.T,
) {
	policy := ConservativeProviderFailoverPolicy{}

	err := &SMSProviderError{
		Provider: "bulksmsiraq",
		Kind:     SMSProviderErrorRateLimited,
	}

	if !policy.ShouldFailover(err) {
		t.Fatal(
			"ShouldFailover() = false for rate-limited provider error, want true",
		)
	}
}

func TestConservativeProviderFailoverPolicyRejectsUnsafeFailures(
	t *testing.T,
) {
	tests := []struct {
		name string
		kind SMSProviderErrorKind
	}{
		{
			name: "permanent",
			kind: SMSProviderErrorPermanent,
		},
		{
			name: "temporary",
			kind: SMSProviderErrorTemporary,
		},
		{
			name: "unknown delivery state",
			kind: SMSProviderErrorUnknownDeliveryState,
		},
	}

	policy := ConservativeProviderFailoverPolicy{}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				err := &SMSProviderError{
					Provider: "bulksmsiraq",
					Kind:     test.kind,
				}

				if policy.ShouldFailover(err) {
					t.Fatalf(
						"ShouldFailover() = true for %q, want false",
						test.kind,
					)
				}
			},
		)
	}
}

func TestConservativeProviderFailoverPolicyRejectsGenericError(
	t *testing.T,
) {
	policy := ConservativeProviderFailoverPolicy{}

	if policy.ShouldFailover(
		errors.New("provider failed"),
	) {
		t.Fatal(
			"ShouldFailover() = true for generic error, want false",
		)
	}
}

func TestConservativeProviderFailoverPolicyRejectsNilError(
	t *testing.T,
) {
	policy := ConservativeProviderFailoverPolicy{}

	if policy.ShouldFailover(nil) {
		t.Fatal(
			"ShouldFailover() = true for nil error, want false",
		)
	}
}
