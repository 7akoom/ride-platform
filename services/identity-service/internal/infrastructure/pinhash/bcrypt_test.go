package pinhash

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestAPINMatchesOnlyItsOwnIdentity(t *testing.T) {
	h := New(bcrypt.MinCost)

	hash, err := h.Hash("identity-1", "2580")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		identity, pin string
		want          bool
	}{
		{"identity-1", "2580", true},
		{"identity-1", "2581", false},
		{"identity-2", "2580", false},
	} {
		got, err := h.Compare(hash, tc.identity, tc.pin)
		if err != nil || got != tc.want {
			t.Fatalf("%s %s: %v %v", tc.identity, tc.pin, got, err)
		}
	}

	if _, err := h.Compare("not-a-hash", "identity-1", "2580"); err == nil {
		t.Fatal("a broken hash must be an error, not a mismatch")
	}
}
