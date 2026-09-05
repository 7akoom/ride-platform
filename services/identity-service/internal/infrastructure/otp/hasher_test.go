package otp

import "testing"

func TestHasherHashAndCompare(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const challengeID = "otp_ch_test"
	const code = "252599"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	hash, err := hasher.Hash(
		challengeID,
		code,
	)
	if err != nil {
		t.Fatalf("Hash() returned an error: %v", err)
	}

	if hash == "" {
		t.Fatal("Hash() returned an empty hash")
	}

	if hash == code {
		t.Fatal("Hash() returned the raw OTP code")
	}

	matches, err := hasher.Compare(
		hash,
		challengeID,
		code,
	)
	if err != nil {
		t.Fatalf(
			"Compare() returned an error: %v",
			err,
		)
	}

	if !matches {
		t.Fatal(
			"Compare() rejected the correct OTP",
		)
	}
}

func TestHasherCompareRejectsWrongOTP(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const challengeID = "otp_ch_test"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	hash, err := hasher.Hash(
		challengeID,
		"252599",
	)
	if err != nil {
		t.Fatalf("Hash() returned an error: %v", err)
	}

	matches, err := hasher.Compare(
		hash,
		challengeID,
		"111111",
	)
	if err != nil {
		t.Fatalf(
			"Compare() returned an error for a valid mismatched OTP: %v",
			err,
		)
	}

	if matches {
		t.Fatal(
			"Compare() accepted an incorrect OTP",
		)
	}
}

func TestHasherCompareRejectsEmptyOTP(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const challengeID = "otp_ch_test"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	hash, err := hasher.Hash(
		challengeID,
		"252599",
	)
	if err != nil {
		t.Fatalf("Hash() returned an error: %v", err)
	}

	_, err = hasher.Compare(
		hash,
		challengeID,
		"",
	)
	if err == nil {
		t.Fatal("Compare() accepted an empty OTP")
	}
}

func TestHasherBindsOTPToChallenge(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const code = "252599"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	firstHash, err := hasher.Hash(
		"otp_ch_first",
		code,
	)
	if err != nil {
		t.Fatalf(
			"Hash() returned an error for first challenge: %v",
			err,
		)
	}

	secondHash, err := hasher.Hash(
		"otp_ch_second",
		code,
	)
	if err != nil {
		t.Fatalf(
			"Hash() returned an error for second challenge: %v",
			err,
		)
	}

	if firstHash == secondHash {
		t.Fatal(
			"Hash() returned the same hash for different challenges",
		)
	}

	matches, err := hasher.Compare(
		firstHash,
		"otp_ch_second",
		code,
	)
	if err != nil {
		t.Fatalf(
			"Compare() returned an error for a challenge mismatch: %v",
			err,
		)
	}

	if matches {
		t.Fatal(
			"Compare() accepted an OTP hash from a different challenge",
		)
	}
}

func TestHasherRejectsEmptyChallengeID(t *testing.T) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const code = "252599"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf("NewHasher() returned an error: %v", err)
	}

	if _, err := hasher.Hash(
		"",
		code,
	); err == nil {
		t.Fatal("Hash() accepted an empty challenge ID")
	}

	hash, err := hasher.Hash(
		"otp_ch_test",
		code,
	)
	if err != nil {
		t.Fatalf("Hash() returned an error: %v", err)
	}

	_, err = hasher.Compare(
		hash,
		"",
		code,
	)
	if err == nil {
		t.Fatal(
			"Compare() accepted an empty challenge ID",
		)
	}
}

func TestHasherCompareReturnsErrorForCorruptedHash(
	t *testing.T,
) {
	const secret = "this-is-a-development-secret-32-bytes-minimum"
	const challengeID = "otp_ch_test"
	const code = "252599"

	hasher, err := NewHasher(secret)
	if err != nil {
		t.Fatalf(
			"NewHasher() returned an error: %v",
			err,
		)
	}

	matches, err := hasher.Compare(
		"%%%not-valid-base64%%%",
		challengeID,
		code,
	)

	if err == nil {
		t.Fatal(
			"Compare() returned nil error for a corrupted OTP hash",
		)
	}

	if matches {
		t.Fatal(
			"Compare() matched a corrupted OTP hash",
		)
	}
}

func TestNewHasherRejectsShortSecret(t *testing.T) {
	if _, err := NewHasher("too-short"); err == nil {
		t.Fatal(
			"NewHasher() accepted a secret shorter than 32 bytes",
		)
	}
}
