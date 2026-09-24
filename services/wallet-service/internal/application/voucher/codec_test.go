package voucher

import (
	"bytes"
	"strings"
	"testing"
)

func TestCodesAreRandomFromTheAlphabetAndTypedLoosely(t *testing.T) {
	codec, err := NewCodec("unit-test-key")
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}

	for i := 0; i < 500; i++ {
		code, err := codec.NewCode()
		if err != nil {
			t.Fatal(err)
		}

		if len(code) != codeLength || strings.Trim(code, codeAlphabet) != "" {
			t.Fatalf("code %q", code)
		}

		if seen[code] {
			t.Fatalf("code %q twice", code)
		}

		seen[code] = true
	}

	for typed, want := range map[string]string{
		"ABCD-EFGH-JKMN-PQRS":     "ABCDEFGHJKMNPQRS",
		" abcd efgh jkmn pqrs ":   "ABCDEFGHJKMNPQRS",
		"abcd-efgh-jkmn-pqrs\t":   "ABCDEFGHJKMNPQRS",
		"ABCD-EFGH-JKMN-PQR":      "",
		"ABCD-EFGH-JKMN-PQRST":    "",
		"ABCD-EFGH-JKMN-PQR0":     "", // 0 is not in the alphabet
		"ABCD-EFGH-JKMN-PQRI":     "",
		"ABCD-EFGH-JKMN-PQRé":     "",
		"ABCD_EFGH_JKMN_PQRS____": "",
	} {
		got, ok := Normalize(typed)
		if ok != (want != "") || (ok && got != want) {
			t.Errorf("Normalize(%q) = %q, %v", typed, got, ok)
		}
	}

	if got := Format("ABCDEFGHJKMNPQRS"); got != "ABCD-EFGH-JKMN-PQRS" {
		t.Fatalf("Format: %q", got)
	}
}

func TestASealedCodeOpensOnlyWithItsKeyAndItsHash(t *testing.T) {
	codec, _ := NewCodec("unit-test-key")
	other, _ := NewCodec("another-key")

	code, _ := codec.NewCode()
	hash := codec.Hash(code)

	if !bytes.Equal(hash, codec.Hash(code)) || bytes.Equal(hash, other.Hash(code)) {
		t.Fatal("the hash must depend on the code and the key only")
	}

	sealed, err := codec.Seal(code, hash)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(sealed, []byte(code)) {
		t.Fatal("the sealed code contains the code")
	}

	if got, err := codec.Open(hash, sealed); err != nil || got != code {
		t.Fatalf("open: %q %v", got, err)
	}

	if _, err := other.Open(hash, sealed); err == nil {
		t.Fatal("another key opened it")
	}

	if _, err := codec.Open(codec.Hash("ABCDEFGHJKMNPQRS"), sealed); err == nil {
		t.Fatal("it opened for another row's hash")
	}

	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 1

	if _, err := codec.Open(hash, tampered); err == nil {
		t.Fatal("a tampered code opened")
	}

	if _, err := codec.Open(hash, sealed[:4]); err == nil {
		t.Fatal("a short box opened")
	}

	if _, err := NewCodec(""); err == nil {
		t.Fatal("an empty key was accepted")
	}
}
