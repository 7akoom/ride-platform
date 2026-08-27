package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type testSMSSender struct {
	calls int
	err   error

	phoneNumber string
	code        string
	purpose     auth.OTPPurpose
}

func (s *testSMSSender) Send(
	ctx context.Context,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
) error {
	s.calls++
	s.phoneNumber = phoneNumber
	s.code = code
	s.purpose = purpose

	return s.err
}

type testEmailSender struct {
	calls int
	err   error

	emailAddress string
	code         string
	purpose      auth.OTPPurpose
}

func (s *testEmailSender) Send(
	ctx context.Context,
	emailAddress string,
	code string,
	purpose auth.OTPPurpose,
) error {
	s.calls++
	s.emailAddress = emailAddress
	s.code = code
	s.purpose = purpose

	return s.err
}

func TestProductionDeliveryRoutesPhoneToSMSSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		emailSender,
	)
	if err != nil {
		t.Fatalf(
			"NewProductionDelivery() returned an error: %v",
			err,
		)
	}

	err = delivery.Send(
		context.Background(),
		auth.OTPDeliveryInput{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "  +9647501234567  ",
			},
			Code:    " 123456 ",
			Purpose: auth.OTPPurposeLogin,
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if smsSender.calls != 1 {
		t.Fatalf(
			"SMS sender calls = %d, expected 1",
			smsSender.calls,
		)
	}

	if emailSender.calls != 0 {
		t.Fatalf(
			"email sender calls = %d, expected 0",
			emailSender.calls,
		)
	}

	if smsSender.phoneNumber != "+9647501234567" {
		t.Fatalf(
			"SMS phone number = %q, expected %q",
			smsSender.phoneNumber,
			"+9647501234567",
		)
	}

	if smsSender.code != "123456" {
		t.Fatalf(
			"SMS code = %q, expected %q",
			smsSender.code,
			"123456",
		)
	}

	if smsSender.purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"SMS purpose = %q, expected %q",
			smsSender.purpose,
			auth.OTPPurposeLogin,
		)
	}
}

func TestProductionDeliveryRoutesEmailToEmailSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		emailSender,
	)
	if err != nil {
		t.Fatalf(
			"NewProductionDelivery() returned an error: %v",
			err,
		)
	}

	err = delivery.Send(
		context.Background(),
		auth.OTPDeliveryInput{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "  User@EXAMPLE.COM  ",
			},
			Code:    " 654321 ",
			Purpose: auth.OTPPurposeLinkIdentifier,
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if emailSender.calls != 1 {
		t.Fatalf(
			"email sender calls = %d, expected 1",
			emailSender.calls,
		)
	}

	if smsSender.calls != 0 {
		t.Fatalf(
			"SMS sender calls = %d, expected 0",
			smsSender.calls,
		)
	}

	if emailSender.emailAddress != "user@example.com" {
		t.Fatalf(
			"email address = %q, expected %q",
			emailSender.emailAddress,
			"user@example.com",
		)
	}

	if emailSender.code != "654321" {
		t.Fatalf(
			"email code = %q, expected %q",
			emailSender.code,
			"654321",
		)
	}

	if emailSender.purpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"email purpose = %q, expected %q",
			emailSender.purpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}
}

func TestProductionDeliveryPropagatesSenderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"SMS provider unavailable",
	)

	smsSender := &testSMSSender{
		err: expectedErr,
	}

	delivery, err := NewProductionDelivery(
		smsSender,
		&testEmailSender{},
	)
	if err != nil {
		t.Fatalf(
			"NewProductionDelivery() returned an error: %v",
			err,
		)
	}

	err = delivery.Send(
		context.Background(),
		auth.OTPDeliveryInput{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Code:    "123456",
			Purpose: auth.OTPPurposeLogin,
		},
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Send() error = %v, expected wrapped sender error",
			err,
		)
	}
}

func TestNewProductionDeliveryRequiresSenders(
	t *testing.T,
) {
	tests := []struct {
		name        string
		smsSender   SMSSender
		emailSender EmailSender
	}{
		{
			name:        "missing SMS sender",
			smsSender:   nil,
			emailSender: &testEmailSender{},
		},
		{
			name:        "missing email sender",
			smsSender:   &testSMSSender{},
			emailSender: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			delivery, err := NewProductionDelivery(
				testCase.smsSender,
				testCase.emailSender,
			)

			if err == nil {
				t.Fatal(
					"NewProductionDelivery() accepted missing sender",
				)
			}

			if delivery != nil {
				t.Fatal(
					"NewProductionDelivery() returned delivery with missing sender",
				)
			}
		})
	}
}
