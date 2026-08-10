package otp

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const otpDigits = 6

var otpUpperBound = big.NewInt(1_000_000)

type Generator struct{}

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) Generate() (string, error) {
	value, err := rand.Int(rand.Reader, otpUpperBound)
	if err != nil {
		return "", fmt.Errorf("generate secure random OTP: %w", err)
	}

	return fmt.Sprintf("%0*d", otpDigits, value.Int64()), nil
}