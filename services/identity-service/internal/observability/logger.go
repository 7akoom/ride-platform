package observability

import (
	"log/slog"
	"os"
	"strings"
)

const redactedValue = "[REDACTED]"

var sensitiveLogKeys = map[string]struct{}{
	"access_token":    {},
	"refresh_token":   {},
	"authorization":   {},
	"password":        {},
	"secret":          {},
	"api_key":         {},
	"cookie":          {},
	"set_cookie":      {},
	"client_secret":   {},
	"private_key":     {},
	"valkey_password": {},
	"otp_hash_secret": {},
}

func NewLogger(
	serviceName string,
	environment string,
) *slog.Logger {
	handler := slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			ReplaceAttr: redactSensitiveAttribute,
		},
	)

	return slog.New(handler).With(
		"service", serviceName,
		"environment", environment,
	)
}

func redactSensitiveAttribute(
	groups []string,
	attr slog.Attr,
) slog.Attr {
	key := strings.ToLower(
		strings.TrimSpace(attr.Key),
	)

	if _, sensitive := sensitiveLogKeys[key]; sensitive {
		attr.Value = slog.StringValue(
			redactedValue,
		)
	}

	return attr
}
