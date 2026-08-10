package token

import (
	"regexp"
	"testing"
)

func TestSessionIDGeneratorGenerateReturnsUUIDv4(t *testing.T) {
	generator := NewSessionIDGenerator()

	sessionID, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}

	if sessionID == "" {
		t.Fatal("Generate() returned an empty session ID")
	}

	uuidV4Pattern := regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)

	if !uuidV4Pattern.MatchString(sessionID) {
		t.Fatalf(
			"Generate() returned invalid UUID v4: %q",
			sessionID,
		)
	}
}