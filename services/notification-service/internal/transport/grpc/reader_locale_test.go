package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestReaderLocale(t *testing.T) {
	cases := map[string]string{
		"ar":              "ar",
		"ar-IQ,en;q=0.8":  "ar",
		" KU ":            "ku",
		"en-US;q=0.9, ar": "en",
		"":                "",
	}

	for header, want := range cases {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("grpcgateway-accept-language", header))

		if got := readerLocale(ctx); got != want {
			t.Fatalf("readerLocale(%q) = %q, want %q", header, got, want)
		}
	}

	if got := readerLocale(context.Background()); got != "" {
		t.Fatalf("without metadata: %q", got)
	}
}
