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
	locale      string
}

func (s *testSMSSender) Send(
	ctx context.Context,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	s.calls++
	s.phoneNumber = phoneNumber
	s.code = code
	s.purpose = purpose
	s.locale = locale

	return s.err
}

type testWhatsAppSender struct {
	calls int
	err   error

	phoneNumber string
	code        string
	purpose     auth.OTPPurpose
	locale      string
}

func (s *testWhatsAppSender) Send(
	ctx context.Context,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	s.calls++
	s.phoneNumber = phoneNumber
	s.code = code
	s.purpose = purpose
	s.locale = locale

	return s.err
}

type testEmailSender struct {
	calls int
	err   error

	emailAddress string
	code         string
	purpose      auth.OTPPurpose
	locale       string
}

func (s *testEmailSender) Send(
	ctx context.Context,
	emailAddress string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	s.calls++
	s.emailAddress = emailAddress
	s.code = code
	s.purpose = purpose
	s.locale = locale

	return s.err
}

func TestProductionDeliveryRoutesAutoPhoneToSMSSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	whatsAppSender := &testWhatsAppSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		whatsAppSender,
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
			Channel: auth.OTPDeliveryChannelAuto,
			Locale:  "ar",
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

	if whatsAppSender.calls != 0 {
		t.Fatalf(
			"WhatsApp sender calls = %d, expected 0",
			whatsAppSender.calls,
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

	if smsSender.locale != "ar" {
		t.Fatalf(
			"SMS locale = %q, expected %q",
			smsSender.locale,
			"ar",
		)
	}
}

func TestProductionDeliveryRoutesAutoEmailToEmailSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	whatsAppSender := &testWhatsAppSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		whatsAppSender,
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
			Channel: auth.OTPDeliveryChannelAuto,
			Locale:  "ku",
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

	if whatsAppSender.calls != 0 {
		t.Fatalf(
			"WhatsApp sender calls = %d, expected 0",
			whatsAppSender.calls,
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

	if emailSender.locale != "ku" {
		t.Fatalf(
			"email locale = %q, expected %q",
			emailSender.locale,
			"ku",
		)
	}
}

func TestProductionDeliveryRoutesExplicitSMSToSMSSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	whatsAppSender := &testWhatsAppSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		whatsAppSender,
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
				Value: "+9647501234567",
			},
			Code:    "123456",
			Purpose: auth.OTPPurposeLogin,
			Channel: auth.OTPDeliveryChannelSMS,
			Locale:  "en",
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

	if whatsAppSender.calls != 0 {
		t.Fatalf(
			"WhatsApp sender calls = %d, expected 0",
			whatsAppSender.calls,
		)
	}

	if emailSender.calls != 0 {
		t.Fatalf(
			"email sender calls = %d, expected 0",
			emailSender.calls,
		)
	}
}

func TestProductionDeliveryRoutesWhatsAppToWhatsAppSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	whatsAppSender := &testWhatsAppSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		whatsAppSender,
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
			Code:    " 987654 ",
			Purpose: auth.OTPPurposeUnlinkIdentifier,
			Channel: auth.OTPDeliveryChannelWhatsApp,
			Locale:  "ar",
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if whatsAppSender.calls != 1 {
		t.Fatalf(
			"WhatsApp sender calls = %d, expected 1",
			whatsAppSender.calls,
		)
	}

	if smsSender.calls != 0 {
		t.Fatalf(
			"SMS sender calls = %d, expected 0",
			smsSender.calls,
		)
	}

	if emailSender.calls != 0 {
		t.Fatalf(
			"email sender calls = %d, expected 0",
			emailSender.calls,
		)
	}

	if whatsAppSender.phoneNumber != "+9647501234567" {
		t.Fatalf(
			"WhatsApp phone number = %q, expected %q",
			whatsAppSender.phoneNumber,
			"+9647501234567",
		)
	}

	if whatsAppSender.code != "987654" {
		t.Fatalf(
			"WhatsApp code = %q, expected %q",
			whatsAppSender.code,
			"987654",
		)
	}

	if whatsAppSender.purpose !=
		auth.OTPPurposeUnlinkIdentifier {
		t.Fatalf(
			"WhatsApp purpose = %q, expected %q",
			whatsAppSender.purpose,
			auth.OTPPurposeUnlinkIdentifier,
		)
	}

	if whatsAppSender.locale != "ar" {
		t.Fatalf(
			"WhatsApp locale = %q, expected %q",
			whatsAppSender.locale,
			"ar",
		)
	}
}

func TestProductionDeliveryRoutesExplicitEmailToEmailSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	whatsAppSender := &testWhatsAppSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		whatsAppSender,
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
				Value: "User@EXAMPLE.COM",
			},
			Code:    "123456",
			Purpose: auth.OTPPurposeLogin,
			Channel: auth.OTPDeliveryChannelEmail,
			Locale:  "en",
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

	if whatsAppSender.calls != 0 {
		t.Fatalf(
			"WhatsApp sender calls = %d, expected 0",
			whatsAppSender.calls,
		)
	}
}

func TestProductionDeliveryRejectsIncompatibleChannelAndIdentifier(
	t *testing.T,
) {
	tests := []struct {
		name       string
		identifier auth.Identifier
		channel    auth.OTPDeliveryChannel
	}{
		{
			name: "SMS with email identifier",
			identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "user@example.com",
			},
			channel: auth.OTPDeliveryChannelSMS,
		},
		{
			name: "WhatsApp with email identifier",
			identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "user@example.com",
			},
			channel: auth.OTPDeliveryChannelWhatsApp,
		},
		{
			name: "email with phone identifier",
			identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647501234567",
			},
			channel: auth.OTPDeliveryChannelEmail,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			smsSender := &testSMSSender{}
			whatsAppSender := &testWhatsAppSender{}
			emailSender := &testEmailSender{}

			delivery, err := NewProductionDelivery(
				smsSender,
				whatsAppSender,
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
					Identifier: testCase.identifier,
					Code:       "123456",
					Purpose:    auth.OTPPurposeLogin,
					Channel:    testCase.channel,
					Locale:     "en",
				},
			)

			if !errors.Is(
				err,
				auth.ErrInvalidOTPDeliveryChannel,
			) {
				t.Fatalf(
					"Send() error = %v, expected %v",
					err,
					auth.ErrInvalidOTPDeliveryChannel,
				)
			}

			if smsSender.calls != 0 {
				t.Fatalf(
					"SMS sender calls = %d, expected 0",
					smsSender.calls,
				)
			}

			if whatsAppSender.calls != 0 {
				t.Fatalf(
					"WhatsApp sender calls = %d, expected 0",
					whatsAppSender.calls,
				)
			}

			if emailSender.calls != 0 {
				t.Fatalf(
					"email sender calls = %d, expected 0",
					emailSender.calls,
				)
			}
		})
	}
}

func TestProductionDeliveryPropagatesSenderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"provider unavailable",
	)

	tests := []struct {
		name           string
		channel        auth.OTPDeliveryChannel
		identifier     auth.Identifier
		smsSender      *testSMSSender
		whatsAppSender *testWhatsAppSender
		emailSender    *testEmailSender
	}{
		{
			name:    "SMS sender error",
			channel: auth.OTPDeliveryChannelSMS,
			identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647501234567",
			},
			smsSender: &testSMSSender{
				err: expectedErr,
			},
			whatsAppSender: &testWhatsAppSender{},
			emailSender:    &testEmailSender{},
		},
		{
			name:    "WhatsApp sender error",
			channel: auth.OTPDeliveryChannelWhatsApp,
			identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647501234567",
			},
			smsSender: &testSMSSender{},
			whatsAppSender: &testWhatsAppSender{
				err: expectedErr,
			},
			emailSender: &testEmailSender{},
		},
		{
			name:    "email sender error",
			channel: auth.OTPDeliveryChannelEmail,
			identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "user@example.com",
			},
			smsSender:      &testSMSSender{},
			whatsAppSender: &testWhatsAppSender{},
			emailSender: &testEmailSender{
				err: expectedErr,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			delivery, err := NewProductionDelivery(
				testCase.smsSender,
				testCase.whatsAppSender,
				testCase.emailSender,
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
					Identifier: testCase.identifier,
					Code:       "123456",
					Purpose:    auth.OTPPurposeLogin,
					Channel:    testCase.channel,
				},
			)

			if !errors.Is(err, expectedErr) {
				t.Fatalf(
					"Send() error = %v, expected wrapped sender error",
					err,
				)
			}
		})
	}
}

func TestNewProductionDeliveryAllowsMissingWhatsAppSender(
	t *testing.T,
) {
	smsSender := &testSMSSender{}
	emailSender := &testEmailSender{}

	delivery, err := NewProductionDelivery(
		smsSender,
		nil,
		emailSender,
	)
	if err != nil {
		t.Fatalf(
			"NewProductionDelivery() rejected missing optional WhatsApp sender: %v",
			err,
		)
	}

	if delivery == nil {
		t.Fatal(
			"NewProductionDelivery() returned nil delivery",
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
			Channel: auth.OTPDeliveryChannelWhatsApp,
			Locale:  "en",
		},
	)

	if !errors.Is(
		err,
		auth.ErrOTPDeliveryChannelUnavailable,
	) {
		t.Fatalf(
			"Send() error = %v, expected %v",
			err,
			auth.ErrOTPDeliveryChannelUnavailable,
		)
	}

	if smsSender.calls != 0 {
		t.Fatalf(
			"SMS sender calls = %d, expected 0",
			smsSender.calls,
		)
	}

	if emailSender.calls != 0 {
		t.Fatalf(
			"email sender calls = %d, expected 0",
			emailSender.calls,
		)
	}
}

func TestNewProductionDeliveryRequiresSMSAndEmailSenders(
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
				nil,
				testCase.emailSender,
			)

			if err == nil {
				t.Fatal(
					"NewProductionDelivery() accepted missing required sender",
				)
			}

			if delivery != nil {
				t.Fatal(
					"NewProductionDelivery() returned delivery with missing required sender",
				)
			}
		})
	}
}
