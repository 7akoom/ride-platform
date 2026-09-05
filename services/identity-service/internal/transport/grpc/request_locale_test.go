package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestRequestLocaleFromIncomingContext(
	t *testing.T,
) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "Arabic",
			header:   "ar",
			expected: "ar",
		},
		{
			name:     "Arabic Iraq",
			header:   "ar-IQ",
			expected: "ar",
		},
		{
			name:     "Kurdish Iraq",
			header:   "ku-IQ",
			expected: "ku",
		},
		{
			name:     "Kurdish underscore",
			header:   "ku_IQ",
			expected: "ku",
		},
		{
			name:     "English",
			header:   "en-US",
			expected: "en",
		},
		{
			name:     "quality preference",
			header:   "en;q=0.5,ku-IQ;q=0.9,ar;q=0.7",
			expected: "ku",
		},
		{
			name:     "unsupported defaults to English",
			header:   "fr-FR",
			expected: "en",
		},
		{
			name:     "blank defaults to English",
			header:   "",
			expected: "en",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs(
					"accept-language",
					testCase.header,
				),
			)

			actual :=
				requestLocaleFromIncomingContext(
					ctx,
				)

			if actual != testCase.expected {
				t.Fatalf(
					"locale = %q, expected %q",
					actual,
					testCase.expected,
				)
			}
		})
	}
}

func TestRequestLocaleDefaultsToEnglishWithoutMetadata(
	t *testing.T,
) {
	actual := requestLocaleFromIncomingContext(
		context.Background(),
	)

	if actual != "en" {
		t.Fatalf(
			"locale = %q, expected %q",
			actual,
			"en",
		)
	}
}
