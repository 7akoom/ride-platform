package observability

import (
	"log/slog"
	"os"
)

func NewLogger(serviceName, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, nil)

	return slog.New(handler).With(
		"service", serviceName,
		"environment", environment,
	)
}