package postgres

import (
	"os"
	"testing"
)

func readMigration(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the migration: %v", err)
	}

	return string(data)
}
