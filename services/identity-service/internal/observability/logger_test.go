package observability

import (
	"log/slog"
	"testing"
)

func TestRedactSensitiveAttributeRedactsSensitiveValues(
	t *testing.T,
) {
	tests := []struct {
		name string
		key  string
	}{
		{
			name: "refresh token",
			key:  "refresh_token",
		},
		{
			name: "access token",
			key:  "access_token",
		},
		{
			name: "authorization",
			key:  "authorization",
		},
		{
			name: "password",
			key:  "password",
		},
		{
			name: "mixed case and surrounding spaces",
			key:  " Refresh_Token ",
		},
		{
			name: "provider API key",
			key:  "telnyx_api_key",
		},
		{
			name: "provider access token",
			key:  "meta_whatsapp_access_token",
		},
		{
			name: "camel case access token",
			key:  "accessToken",
		},
		{
			name: "camel case refresh token",
			key:  "refreshToken",
		},
		{
			name: "bearer token",
			key:  "bearer_token",
		},
		{
			name: "authorization header",
			key:  "authorization_header",
		},
		{
			name: "hyphenated API key header",
			key:  "x-api-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := slog.String(
				tt.key,
				"super-sensitive-value",
			)

			redacted := redactSensitiveAttribute(
				nil,
				attr,
			)

			if redacted.Value.String() != redactedValue {
				t.Fatalf(
					"redacted value = %q, expected %q",
					redacted.Value.String(),
					redactedValue,
				)
			}
		})
	}
}

func TestRedactSensitiveAttributePreservesNonSensitiveValues(
	t *testing.T,
) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "OTP code remains available for development delivery",
			key:   "otp_code",
			value: "123456",
		},
		{
			name:  "OTP identifier remains available for development delivery",
			key:   "otp_identifier",
			value: "+9647500000000",
		},
		{
			name:  "ordinary field",
			key:   "grpc_address",
			value: ":50051",
		},
		{
			name:  "non-sensitive token metadata",
			key:   "token_count",
			value: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := slog.String(
				tt.key,
				tt.value,
			)

			result := redactSensitiveAttribute(
				nil,
				attr,
			)

			if result.Value.String() != tt.value {
				t.Fatalf(
					"attribute value = %q, expected %q",
					result.Value.String(),
					tt.value,
				)
			}
		})
	}
}
