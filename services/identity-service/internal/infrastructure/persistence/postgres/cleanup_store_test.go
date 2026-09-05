package postgres

import "testing"

func TestNewCleanupStorePanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewCleanupStore() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewCleanupStore(nil)
}
