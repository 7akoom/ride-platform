package main

import (
	"net/http"
	"strings"
)

// providerWebhookPaths are called by payment providers, not by users. A provider
// proves who it is in its own way (a signed body, a shared secret) and does not
// carry a user access token, so the header shape check below cannot apply to it;
// the service behind the route verifies the provider itself.
var providerWebhookPaths = map[string]struct{}{
	"/v1/wallet/zaincash/webhook": {},
}

// requireUserCredentials keeps the public gateway from carrying anything but an
// end user's access token to the backends.
//
// Every backend accepts a second credential in the same Authorization header:
// the shared internal service token, which bypasses every ownership check. That
// token exists for service-to-service calls and must never be usable from the
// internet. A user access token is a JWT (three dot-separated base64url parts);
// the internal token is a random secret with no dots, so anything else in the
// header is refused here, before it can be forwarded.
//
// grpc-gateway also turns any "Grpc-Metadata-*" request header into gRPC
// metadata (Grpc-Metadata-Authorization becomes authorization). Clients never
// need that, and it would be a second way to smuggle a credential past the check
// above, so those headers are dropped.
func requireUserCredentials(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if strings.HasPrefix(strings.ToLower(name), "grpc-metadata-") {
				r.Header.Del(name)
			}
		}

		_, providerCall := providerWebhookPaths[r.URL.Path]

		if header := r.Header.Get("Authorization"); !providerCall && header != "" && !isBearerJWT(header) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":16,"message":"a user access token is required","details":[]}`))

			return
		}

		next.ServeHTTP(w, r)
	})
}

// isBearerJWT reports whether the header is exactly "Bearer <a.b.c>" with three
// non-empty base64url segments.
func isBearerJWT(header string) bool {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}

		for _, r := range part {
			isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			isDigit := r >= '0' && r <= '9'

			if !isLetter && !isDigit && r != '-' && r != '_' {
				return false
			}
		}
	}

	return true
}
