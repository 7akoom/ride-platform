package notification

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// A page token is opaque to clients: base64url of "<created_at unix nanos>:<id>".

func encodePageToken(cursor PageCursor) string {
	raw := strconv.FormatInt(cursor.CreatedAt.UnixNano(), 10) + ":" + cursor.ID

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodePageToken(token string) (*PageCursor, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidPageToken
	}

	nanos, id, found := strings.Cut(string(raw), ":")
	if !found || id == "" || len(id) > 64 {
		return nil, ErrInvalidPageToken
	}

	value, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil || value <= 0 {
		return nil, ErrInvalidPageToken
	}

	return &PageCursor{CreatedAt: time.Unix(0, value).UTC(), ID: id}, nil
}
