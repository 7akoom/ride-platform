package postgres

import (
	"context"
	"encoding/json"
	"testing"
)

// Every section is valid JSON (an array), even for a person with nothing here.
func TestPersonalDataExportSectionsAreJSON(t *testing.T) {
	pool := testPool(t)

	sections, err := NewPersonalDataExporter(pool).Export(context.Background(), "7d1e2f30-0000-4000-8000-00000000a001", "7d1e2f30-0000-4000-8000-00000000a002", "7d1e2f30-0000-4000-8000-00000000a003")
	if err != nil {
		t.Fatal(err)
	}

	if len(sections) != len(personalDataQueries) {
		t.Fatalf("got %d sections, want %d", len(sections), len(personalDataQueries))
	}

	for _, s := range sections {
		var rows []map[string]any
		if err := json.Unmarshal(s.Content, &rows); err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
	}
}
