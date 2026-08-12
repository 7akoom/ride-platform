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

func (h *Hasher) Hash(
	challengeID string,
	code string,
) (string, error) {
	if challengeID == "" {
		return "", errors.New("OTP challenge ID cannot be empty")
	}

	if code == "" {
		return "", errors.New("OTP code cannot be empty")
	}

	hash, err := h.computeHash(
		challengeID,
		code,
	)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(hash), nil
}

func (h *Hasher) Compare(
	hash string,
	challengeID string,
	code string,
) (bool, error) {
	if challengeID == "" {
		return false, errors.New(
			"OTP challenge ID cannot be empty",
		)
	}

	if code == "" {
		return false, errors.New(
			"OTP code cannot be empty",
		)
	}

	expectedHash, err := base64.RawURLEncoding.DecodeString(
		hash,
	)
	if err != nil {
		return false, fmt.Errorf(
			"decode OTP hash: %w",
			err,
		)
	}

	actualHash, err := h.computeHash(
		challengeID,
		code,
	)
	if err != nil {
		return false, err
	}

	if !hmac.Equal(
		expectedHash,
		actualHash,
	) {
		return false, nil
	}

	return true, nil
}

func (h *Hasher) computeHash(
	challengeID string,
	code string,
) ([]byte, error) {
	mac := hmac.New(
		sha256.New,
		h.secret,
	)

	if _, err := fmt.Fprintf(
		mac,
		"otp:v1:%d:%s:%d:%s",
		len(challengeID),
		challengeID,
		len(code),
		code,
	); err != nil {
		return nil, fmt.Errorf(
			"write challenge-bound OTP to HMAC: %w",
			err,
		)
	}

	return mac.Sum(nil), nil
}
