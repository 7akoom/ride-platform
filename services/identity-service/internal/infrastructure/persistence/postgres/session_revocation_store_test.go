package postgres

import "testing"

func TestNewSessionRevocationStorePanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewSessionRevocationStore() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewSessionRevocationStore(nil)
}
