package otp

import "testing"

func TestHasherHashAndCompare(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const code = "252599"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	hash, err := hasher.Hash(code)
	if err != nil {
		t.Fatalf("Hash() returned an error: %v", err)
	}

	if hash == "" {
		t.Fatal("Hash() returned an empty hash")
	}

	if hash == code {
		t.Fatal("Hash() returned the raw OTP code")
	}

	if err := hasher.Compare(hash, code); err != nil {
		t.Fatalf("Compare() rejected the correct OTP: %v", err)
	}
}

func TestHasherCompareRejectsWrongOTP(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	hash, err := hasher.Hash("252599")
	if err != nil {
		t.Fatalf("Hash() returned an error: %v", err)
	}

	if err := hasher.Compare(hash, "111111"); err == nil {
		t.Fatal("Compare() accepted an incorrect OTP")
	}
}

func TestNewHasherRejectsShortSecret(t *testing.T) {
	if _, err := NewHasher("too-short"); err == nil {
		t.Fatal("NewHasher() accepted a secret shorter than 32 bytes")
	}
}