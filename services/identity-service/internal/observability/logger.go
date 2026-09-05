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
	key := normalizeLogKey(attr.Key)

	if isSensitiveLogKey(key) {
		attr.Value = slog.StringValue(
			redactedValue,
		)
	}

	return attr
}

func normalizeLogKey(value string) string {
	value = strings.ToLower(
		strings.TrimSpace(value),
	)

	replacer := strings.NewReplacer(
		"-", "_",
		".", "_",
		" ", "_",
	)

	return replacer.Replace(value)
}

func isSensitiveLogKey(key string) bool {
	if _, sensitive := sensitiveLogKeys[key]; sensitive {
		return true
	}

	compactKey := strings.NewReplacer(
		"_", "",
		"-", "",
		".", "",
		" ", "",
	).Replace(key)

	switch {
	case strings.HasSuffix(key, "_token"),
		strings.HasSuffix(compactKey, "token"):
		return true

	case strings.HasSuffix(key, "_api_key"),
		strings.HasSuffix(compactKey, "apikey"):
		return true

	case strings.HasPrefix(key, "authorization_"),
		strings.HasSuffix(key, "_authorization"),
		compactKey == "authorizationheader":
		return true

	case strings.HasSuffix(key, "_password"),
		strings.HasSuffix(compactKey, "password"):
		return true

	case strings.HasSuffix(key, "_secret"),
		strings.HasSuffix(compactKey, "secret"):
		return true

	case strings.HasSuffix(key, "_cookie"),
		strings.HasSuffix(compactKey, "cookie"):
		return true

	case strings.HasSuffix(key, "_private_key"),
		strings.HasSuffix(compactKey, "privatekey"):
		return true
	}

	return false
}
