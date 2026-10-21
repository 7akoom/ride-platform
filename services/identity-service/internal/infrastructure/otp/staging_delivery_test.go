package otp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type stagingTestLog struct{ sent []auth.OTPDeliveryInput }

func (l *stagingTestLog) Send(_ context.Context, input auth.OTPDeliveryInput) error {
	l.sent = append(l.sent, input)

	return nil
}

type stagingTestWhatsApp struct {
	phones []string
	err    error
}

func (w *stagingTestWhatsApp) Send(_ context.Context, phone, _ string, _ auth.OTPPurpose, _ string) error {
	w.phones = append(w.phones, phone)

	return w.err
}

func TestStagingDeliveryLogsAndSendsPhoneCodes(t *testing.T) {
	log := &stagingTestLog{}
	whatsApp := &stagingTestWhatsApp{err: errors.New("no balance")}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	delivery, err := NewStagingDelivery("test", log, whatsApp, logger)
	if err != nil {
		t.Fatalf("NewStagingDelivery: %v", err)
	}

	phone := func(value string, channel auth.OTPDeliveryChannel) auth.OTPDeliveryInput {
		return auth.OTPDeliveryInput{
			Identifier: auth.Identifier{Type: auth.IdentifierTypePhone, Value: value},
			Code:       "123456",
			Channel:    channel,
		}
	}

	inputs := []auth.OTPDeliveryInput{
		phone("+9647501234567", auth.OTPDeliveryChannelWhatsApp),
		phone("+9647501234568", auth.OTPDeliveryChannelAuto),
		phone("+9647700000992", auth.OTPDeliveryChannelWhatsApp), // the dev scripts' test rider
		{Identifier: auth.Identifier{Type: auth.IdentifierTypeEmail, Value: "a@b.iq"}, Code: "1", Channel: auth.OTPDeliveryChannelEmail},
	}

	for _, input := range inputs {
		// A WhatsApp failure never stops the sign-in on staging.
		if err := delivery.Send(context.Background(), input); err != nil {
			t.Fatalf("Send(%s): %v", input.Identifier.Value, err)
		}
	}

	if len(log.sent) != len(inputs) {
		t.Fatalf("%d codes in the log, want %d", len(log.sent), len(inputs))
	}

	if len(whatsApp.phones) != 2 || whatsApp.phones[0] != "+9647501234567" || whatsApp.phones[1] != "+9647501234568" {
		t.Fatalf("WhatsApp got %v", whatsApp.phones)
	}
}

func TestStagingDeliveryIsNotForProduction(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewStagingDelivery("production", &stagingTestLog{}, &stagingTestWhatsApp{}, logger); err == nil {
		t.Fatal("staging delivery was built for production")
	}

	if _, err := NewStagingDelivery("test", &stagingTestLog{}, nil, logger); err == nil {
		t.Fatal("staging delivery was built without WhatsApp")
	}
}
