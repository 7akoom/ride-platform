package otp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

const minimumHashSecretLength = 32

type Hasher struct {
	secret []byte
}

func NewHasher(secret string) (*Hasher, error) {
	if len(secret) < minimumHashSecretLength {
		return nil, errors.New("OTP hash secret must be at least 32 bytes")
	}

	return &Hasher{
		secret: []byte(secret),
	}, nil
}

func (h *Hasher) Hash(code string) (string, error) {
	if code == "" {
		return "", errors.New("OTP code cannot be empty")
	}

	mac := hmac.New(sha256.New, h.secret)

	if _, err := mac.Write([]byte(code)); err != nil {
		return "", fmt.Errorf("write OTP to HMAC: %w", err)
	}

	hash := mac.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(hash), nil
}

func (h *Hasher) Compare(hash, code string) error {
	expectedHash, err := base64.RawURLEncoding.DecodeString(hash)
	if err != nil {
		return fmt.Errorf("decode OTP hash: %w", err)
	}

	mac := hmac.New(sha256.New, h.secret)

	if _, err := mac.Write([]byte(code)); err != nil {
		return fmt.Errorf("write OTP to HMAC: %w", err)
	}

	actualHash := mac.Sum(nil)

	if !hmac.Equal(expectedHash, actualHash) {
		return errors.New("OTP does not match")
	}

	return nil
}