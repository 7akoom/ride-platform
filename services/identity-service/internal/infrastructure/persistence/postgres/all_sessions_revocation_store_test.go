package postgres

import "testing"

func TestNewAllSessionsRevocationStorePanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewAllSessionsRevocationStore() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewAllSessionsRevocationStore(nil)
}
