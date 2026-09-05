package valkey

import "testing"

func TestNewSessionAccessRevocationStorePanicsWhenClientIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewSessionAccessRevocationStore() did not panic for nil client",
			)
		}
	}()

	NewSessionAccessRevocationStore(nil)
}
