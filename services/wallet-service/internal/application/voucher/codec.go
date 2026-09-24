package voucher

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const (
	codeLength = 16
	// codeAlphabet leaves out 0/O and 1/I/L, so a code read off a card is not
	// mistaken. 31 symbols, 16 of them: about 79 bits.
	codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

var errSealedCode = errors.New("a sealed voucher code could not be opened (VOUCHER_CODE_KEY changed?)")

// Codec makes, hashes and seals codes with keys derived from the
// deployment's VOUCHER_CODE_KEY. Changing that key makes every code issued
// before it unredeemable, and every batch not exported yet unexportable.
type Codec struct {
	lookupKey []byte
	seal      cipher.AEAD
}

func NewCodec(key string) (*Codec, error) {
	if key == "" {
		return nil, errors.New("the voucher code key is empty")
	}

	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(label))

		return mac.Sum(nil)
	}

	block, err := aes.NewCipher(derive("ride-voucher-seal-v1"))
	if err != nil {
		return nil, fmt.Errorf("voucher seal cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("voucher seal cipher: %w", err)
	}

	return &Codec{lookupKey: derive("ride-voucher-lookup-v1"), seal: aead}, nil
}

// NewCode is a fresh random code (normalized: 16 symbols, no dashes).
func (c *Codec) NewCode() (string, error) {
	limit := big.NewInt(int64(len(codeAlphabet)))
	out := make([]byte, codeLength)

	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate a voucher code: %w", err)
		}

		out[i] = codeAlphabet[n.Int64()]
	}

	return string(out), nil
}

// Normalize turns what a rider typed into a code: case, spaces and dashes do
// not matter. ok is false for anything that cannot be a code.
func Normalize(typed string) (string, bool) {
	var b strings.Builder

	for _, r := range strings.ToUpper(typed) {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r < 128 && strings.IndexByte(codeAlphabet, byte(r)) >= 0:
			b.WriteRune(r)
		default:
			return "", false
		}
	}

	code := b.String()

	return code, len(code) == codeLength
}

// Format groups a normalized code in fours: ABCD-EFGH-JKMN-PQRS.
func Format(code string) string {
	parts := make([]string, 0, 4)

	for i := 0; i < len(code); i += 4 {
		parts = append(parts, code[i:min(i+4, len(code))])
	}

	return strings.Join(parts, "-")
}

// Hash is what a normalized code is stored and found by.
func (c *Codec) Hash(code string) []byte {
	mac := hmac.New(sha256.New, c.lookupKey)
	mac.Write([]byte(code))

	return mac.Sum(nil)
}

// Seal encrypts a code, bound to its hash.
func (c *Codec) Seal(code string, hash []byte) ([]byte, error) {
	nonce := make([]byte, c.seal.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("voucher seal nonce: %w", err)
	}

	return c.seal.Seal(nonce, nonce, []byte(code), hash), nil
}

// Open decrypts a sealed code and checks it still hashes to its row.
func (c *Codec) Open(hash, sealed []byte) (string, error) {
	size := c.seal.NonceSize()
	if len(sealed) <= size {
		return "", errSealedCode
	}

	plain, err := c.seal.Open(nil, sealed[:size], sealed[size:], hash)
	if err != nil {
		return "", errSealedCode
	}

	code := string(plain)
	if !hmac.Equal(c.Hash(code), hash) {
		return "", errSealedCode
	}

	return code, nil
}
