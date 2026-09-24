// Package pinhash hashes wallet PINs with bcrypt. A PIN has few possible
// values, so the hash alone could not keep it secret from someone holding
// the database: what protects it is the lock after wrong attempts. The slow
// hash still makes a stolen table expensive to go through, and binding it to
// the identity id means a hash copied onto another identity never matches.
package pinhash

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/walletpin"
)

// DefaultCost is bcrypt's cost for PINs.
const DefaultCost = 11

type Hasher struct {
	cost int
}

var _ walletpin.Hasher = Hasher{}

func New(cost int) Hasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		panic("bcrypt cost is out of range")
	}

	return Hasher{cost: cost}
}

func (h Hasher) Hash(identityID, pin string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(secret(identityID, pin), h.cost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}

	return string(hash), nil
}

func (h Hasher) Compare(hash, identityID, pin string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), secret(identityID, pin))

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, nil
	default:
		return false, fmt.Errorf("bcrypt: %w", err)
	}
}

func secret(identityID, pin string) []byte {
	return []byte("wallet-pin:" + identityID + ":" + pin)
}
