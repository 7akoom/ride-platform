package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

// readerLocale is the language the caller reads in, from the Accept-Language the
// gateway passes on ("ar", "ku", "en"; "ar-IQ,en;q=0.8" gives "ar"). Empty when
// there is none.
func readerLocale(ctx context.Context) string {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	for _, key := range []string{"grpcgateway-accept-language", "accept-language"} {
		for _, value := range incoming.Get(key) {
			if locale := firstLanguage(value); locale != "" {
				return locale
			}
		}
	}

	return ""
}

func firstLanguage(header string) string {
	first, _, _ := strings.Cut(header, ",")
	first, _, _ = strings.Cut(first, ";")
	first, _, _ = strings.Cut(strings.TrimSpace(first), "-")

	return strings.ToLower(strings.TrimSpace(first))
}
