package token

import (
	"crypto/rand"
	"fmt"
)

const sessionIDBytes = 16

type SessionIDGenerator struct{}

func NewSessionIDGenerator() *SessionIDGenerator {
	return &SessionIDGenerator{}
}

func (g *SessionIDGenerator) Generate() (string, error) {
	randomBytes := make([]byte, sessionIDBytes)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf(
			"generate secure session ID: %w",
			err,
		)
	}

	randomBytes[6] = (randomBytes[6] & 0x0f) | 0x40
	randomBytes[8] = (randomBytes[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		randomBytes[0:4],
		randomBytes[4:6],
		randomBytes[6:8],
		randomBytes[8:10],
		randomBytes[10:16],
	), nil
}