package grpc

import "testing"

func TestNewIdentityHandlerPanicsWhenAuthServiceIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewIdentityHandler() did not panic for nil auth service",
			)
		}
	}()

	NewIdentityHandler(nil)
}
