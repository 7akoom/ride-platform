package identifier

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const challengeIDBytes = 24

type ChallengeIDGenerator struct{}

func NewChallengeIDGenerator() *ChallengeIDGenerator {
	return &ChallengeIDGenerator{}
}

func (g *ChallengeIDGenerator) Generate() (string, error) {
	randomBytes := make([]byte, challengeIDBytes)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate secure random challenge ID: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	return "otp_ch_" + encoded, nil
}