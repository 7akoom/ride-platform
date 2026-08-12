package otp

import (
	"io"
	"log/slog"
	"testing"
)

func TestNewDevelopmentDeliveryAllowsDevelopment(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	delivery, err := NewDevelopmentDelivery(
		"development",
		logger,
	)
	if err != nil {
		t.Fatalf(
			"NewDevelopmentDelivery() rejected development environment: %v",
			err,
		)
	}

	if delivery == nil {
		t.Fatal("NewDevelopmentDelivery() returned nil delivery")
	}
}

func TestNewDevelopmentDeliveryRejectsProduction(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	delivery, err := NewDevelopmentDelivery(
		"production",
		logger,
	)

	if err == nil {
		t.Fatal(
			"NewDevelopmentDelivery() accepted production environment",
		)
	}

	if delivery != nil {
		t.Fatal(
			"NewDevelopmentDelivery() returned delivery for production environment",
		)
	}
}

func TestNewDevelopmentDeliveryRejectsNilLogger(
	t *testing.T,
) {
	delivery, err := NewDevelopmentDelivery(
		"development",
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewDevelopmentDelivery() accepted nil logger",
		)
	}

	if delivery != nil {
		t.Fatal(
			"NewDevelopmentDelivery() returned delivery for nil logger",
		)
	}
}
