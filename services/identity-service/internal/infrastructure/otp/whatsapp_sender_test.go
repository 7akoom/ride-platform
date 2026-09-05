package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type testWhatsAppSenderProvider struct {
	calls int
	input WhatsAppOTPProviderInput
	err   error
}

func (p *testWhatsAppSenderProvider) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	p.calls++
	p.input = input

	return p.err
}

func TestProviderWhatsAppSenderNormalizesAndSendsOTP(
	t *testing.T,
) {
	provider := &testWhatsAppSenderProvider{}

	sender, err := NewProviderWhatsAppSender(
		provider,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderWhatsAppSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		" +9647501234567 ",
		" 123456 ",
		auth.OTPPurposeLogin,
		" ar ",
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if provider.calls != 1 {
		t.Fatalf(
			"provider calls = %d, expected 1",
			provider.calls,
		)
	}

	expectedInput := WhatsAppOTPProviderInput{
		PhoneNumber: "+9647501234567",
		Code:        "123456",
		Purpose:     auth.OTPPurposeLogin,
		Locale:      "ar",
	}

	if provider.input != expectedInput {
		t.Fatalf(
			"provider input = %+v, expected %+v",
			provider.input,
			expectedInput,
		)
	}
}

func TestProviderWhatsAppSenderPropagatesProviderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"provider unavailable",
	)

	provider := &testWhatsAppSenderProvider{
		err: expectedErr,
	}

	sender, err := NewProviderWhatsAppSender(
		provider,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderWhatsAppSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"+9647501234567",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Send() error = %v, expected wrapped provider error",
			err,
		)
	}
}

func TestProviderWhatsAppSenderRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name        string
		phoneNumber string
		code        string
		purpose     auth.OTPPurpose
	}{
		{
			name:        "blank phone number",
			phoneNumber: " ",
			code:        "123456",
			purpose:     auth.OTPPurposeLogin,
		},
		{
			name:        "blank OTP code",
			phoneNumber: "+9647501234567",
			code:        " ",
			purpose:     auth.OTPPurposeLogin,
		},
		{
			name:        "invalid OTP purpose",
			phoneNumber: "+9647501234567",
			code:        "123456",
			purpose:     auth.OTPPurpose("invalid"),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &testWhatsAppSenderProvider{}

			sender, err := NewProviderWhatsAppSender(
				provider,
			)
			if err != nil {
				t.Fatalf(
					"NewProviderWhatsAppSender() returned an error: %v",
					err,
				)
			}

			err = sender.Send(
				context.Background(),
				testCase.phoneNumber,
				testCase.code,
				testCase.purpose,
				"en",
			)

			if err == nil {
				t.Fatal(
					"Send() accepted invalid input",
				)
			}

			if provider.calls != 0 {
				t.Fatalf(
					"provider calls = %d, expected 0",
					provider.calls,
				)
			}
		})
	}
}

func TestNewProviderWhatsAppSenderRequiresProvider(
	t *testing.T,
) {
	sender, err := NewProviderWhatsAppSender(
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewProviderWhatsAppSender() accepted missing provider",
		)
	}

	if sender != nil {
		t.Fatal(
			"NewProviderWhatsAppSender() returned sender with missing provider",
		)
	}
}
