package notification

import (
	"errors"
	"testing"
	"time"
)

func TestPageTokenRoundTrip(t *testing.T) {
	cursor := PageCursor{CreatedAt: time.Date(2026, 10, 4, 12, 30, 15, 123456000, time.UTC), ID: "0b8f9d4e-1111-4222-8333-944455556666"}

	decoded, err := decodePageToken(encodePageToken(cursor))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !decoded.CreatedAt.Equal(cursor.CreatedAt) || decoded.ID != cursor.ID {
		t.Fatalf("got %+v, want %+v", decoded, cursor)
	}
}

func TestPageTokenEmptyAndInvalid(t *testing.T) {
	if cursor, err := decodePageToken(""); err != nil || cursor != nil {
		t.Fatalf("empty token: %v %v", cursor, err)
	}

	for _, token := range []string{"!!!", "bm8tY29sb24", "YWJjOmlk", "MDppZA"} {
		if _, err := decodePageToken(token); !errors.Is(err, ErrInvalidPageToken) {
			t.Fatalf("token %q: want ErrInvalidPageToken, got %v", token, err)
		}
	}
}
