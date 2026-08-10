package identifier

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestChallengeIDGeneratorGenerate(t *testing.T) {
	generator := NewChallengeIDGenerator()

	challengeID, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}

	const prefix = "otp_ch_"

	if !strings.HasPrefix(challengeID, prefix) {
		t.Fatalf(
			"Generate() returned ID without expected prefix: %q",
			challengeID,
		)
	}

	encoded := strings.TrimPrefix(challengeID, prefix)

	randomBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("generated ID contains invalid base64url payload: %v", err)
	}

	if len(randomBytes) != challengeIDBytes {
		t.Fatalf(
			"generated ID contains %d random bytes, expected %d",
			len(randomBytes),
			challengeIDBytes,
		)
	}

	t.Logf("generated challenge ID: %s", challengeID)
}