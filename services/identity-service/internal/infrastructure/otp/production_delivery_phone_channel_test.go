package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func autoPhoneInput(channel auth.OTPDeliveryChannel) auth.OTPDeliveryInput {
	return auth.OTPDeliveryInput{
		Identifier: auth.Identifier{Type: auth.IdentifierTypePhone, Value: "+9647501234567"},
		Code:       "123456",
		Purpose:    auth.OTPPurposeLogin,
		Channel:    channel,
		Locale:     "ar",
	}
}

func TestProductionDeliveryAutoPhoneCanGoByWhatsApp(t *testing.T) {
	sms, whatsApp := &testSMSSender{}, &testWhatsAppSender{}

	delivery, err := NewProductionDelivery(sms, whatsApp, &testEmailSender{},
		WithPhoneAutoChannel(auth.OTPDeliveryChannelWhatsApp))
	if err != nil {
		t.Fatal(err)
	}

	if err := delivery.Send(context.Background(), autoPhoneInput(auth.OTPDeliveryChannelAuto)); err != nil {
		t.Fatal(err)
	}

	if whatsApp.calls != 1 || sms.calls != 0 {
		t.Fatalf("whatsapp %d sms %d, want 1 and 0", whatsApp.calls, sms.calls)
	}

	if whatsApp.code != "123456" || whatsApp.locale != "ar" || whatsApp.phoneNumber != "+9647501234567" {
		t.Fatalf("sent %+v", whatsApp)
	}

	// Asking for SMS by name still goes by SMS.
	if err := delivery.Send(context.Background(), autoPhoneInput(auth.OTPDeliveryChannelSMS)); err != nil || sms.calls != 1 {
		t.Fatalf("explicit SMS: %v, sms calls %d", err, sms.calls)
	}
}

func TestProductionDeliveryWhatsAppOnly(t *testing.T) {
	whatsApp := &testWhatsAppSender{}

	delivery, err := NewProductionDelivery(nil, whatsApp, &testEmailSender{},
		WithPhoneAutoChannel(auth.OTPDeliveryChannelWhatsApp))
	if err != nil {
		t.Fatal(err)
	}

	if err := delivery.Send(context.Background(), autoPhoneInput(auth.OTPDeliveryChannelAuto)); err != nil || whatsApp.calls != 1 {
		t.Fatalf("auto: %v, whatsapp calls %d", err, whatsApp.calls)
	}

	err = delivery.Send(context.Background(), autoPhoneInput(auth.OTPDeliveryChannelSMS))
	if !errors.Is(err, auth.ErrOTPDeliveryChannelUnavailable) {
		t.Fatalf("SMS with no SMS sender: %v, want channel unavailable", err)
	}
}

func TestNewProductionDeliveryChecksThePhoneDefault(t *testing.T) {
	email := &testEmailSender{}

	if d, err := NewProductionDelivery(&testSMSSender{}, nil, email,
		WithPhoneAutoChannel(auth.OTPDeliveryChannelWhatsApp)); err == nil || d != nil {
		t.Fatal("WhatsApp by default without a WhatsApp sender was accepted")
	}

	if d, err := NewProductionDelivery(nil, &testWhatsAppSender{}, email); err == nil || d != nil {
		t.Fatal("SMS by default without an SMS sender was accepted")
	}

	if d, err := NewProductionDelivery(&testSMSSender{}, &testWhatsAppSender{}, email,
		WithPhoneAutoChannel(auth.OTPDeliveryChannelEmail)); err == nil || d != nil {
		t.Fatal("email as the phone default was accepted")
	}

	if d, err := NewProductionDelivery(&testSMSSender{}, nil, email, nil); err == nil || d != nil {
		t.Fatal("a nil option was accepted")
	}
}
