package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type testEmailProvider struct {
	calls   int
	message EmailMessage
	err     error
}

func (p *testEmailProvider) Send(
	ctx context.Context,
	message EmailMessage,
) error {
	p.calls++
	p.message = message

	return p.err
}

type testEmailMessageRenderer struct {
	calls int
	input OTPEmailMessageRenderInput

	result RenderedEmailMessage
	err    error
}

func (r *testEmailMessageRenderer) Render(
	input OTPEmailMessageRenderInput,
) (RenderedEmailMessage, error) {
	r.calls++
	r.input = input

	return r.result, r.err
}

func TestProviderEmailSenderRendersAndSendsMessage(
	t *testing.T,
) {
	provider := &testEmailProvider{}

	renderer := &testEmailMessageRenderer{
		result: RenderedEmailMessage{
			Subject:  "Ride verification code",
			TextBody: "Your code is 123456.",
			HTMLBody: "<p>Your code is 123456.</p>",
		},
	}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"  User@EXAMPLE.COM  ",
		" 123456 ",
		auth.OTPPurposeLogin,
		"ar-IQ",
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if renderer.calls != 1 {
		t.Fatalf(
			"renderer calls = %d, expected 1",
			renderer.calls,
		)
	}

	if renderer.input.Code != "123456" {
		t.Fatalf(
			"renderer code = %q, expected %q",
			renderer.input.Code,
			"123456",
		)
	}

	if renderer.input.Purpose !=
		auth.OTPPurposeLogin {
		t.Fatalf(
			"renderer purpose = %q, expected %q",
			renderer.input.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if renderer.input.Locale != "ar-IQ" {
		t.Fatalf(
			"renderer locale = %q, expected %q",
			renderer.input.Locale,
			"ar-IQ",
		)
	}

	if provider.calls != 1 {
		t.Fatalf(
			"provider calls = %d, expected 1",
			provider.calls,
		)
	}

	if provider.message.To != "user@example.com" {
		t.Fatalf(
			"provider email address = %q, expected %q",
			provider.message.To,
			"user@example.com",
		)
	}

	if provider.message.Subject !=
		"Ride verification code" {
		t.Fatalf(
			"provider subject = %q, expected %q",
			provider.message.Subject,
			"Ride verification code",
		)
	}

	if provider.message.TextBody !=
		"Your code is 123456." {
		t.Fatalf(
			"provider text body = %q, expected %q",
			provider.message.TextBody,
			"Your code is 123456.",
		)
	}

	if provider.message.HTMLBody !=
		"<p>Your code is 123456.</p>" {
		t.Fatalf(
			"provider HTML body = %q, expected %q",
			provider.message.HTMLBody,
			"<p>Your code is 123456.</p>",
		)
	}
}

func TestProviderEmailSenderPropagatesRendererError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"render failed",
	)

	sender, err := NewProviderEmailSender(
		&testEmailProvider{},
		&testEmailMessageRenderer{
			err: expectedErr,
		},
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"user@example.com",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Send() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}
}

func TestProviderEmailSenderPropagatesProviderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"provider unavailable",
	)

	provider := &testEmailProvider{
		err: expectedErr,
	}

	renderer := &testEmailMessageRenderer{
		result: RenderedEmailMessage{
			Subject:  "Ride verification code",
			TextBody: "Your code is 123456.",
		},
	}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"user@example.com",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Send() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}
}

func TestProviderEmailSenderRejectsInvalidEmailAddress(
	t *testing.T,
) {
	provider := &testEmailProvider{}
	renderer := &testEmailMessageRenderer{}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"not-an-email",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)

	if err == nil {
		t.Fatal(
			"Send() accepted invalid email address",
		)
	}

	if provider.calls != 0 {
		t.Fatalf(
			"provider calls = %d, expected 0",
			provider.calls,
		)
	}
}

func TestNewProviderEmailSenderRequiresDependencies(
	t *testing.T,
) {
	renderer := &testEmailMessageRenderer{}
	provider := &testEmailProvider{}

	if _, err := NewProviderEmailSender(
		nil,
		renderer,
	); err == nil {
		t.Fatal(
			"NewProviderEmailSender() accepted nil provider",
		)
	}

	if _, err := NewProviderEmailSender(
		provider,
		nil,
	); err == nil {
		t.Fatal(
			"NewProviderEmailSender() accepted nil renderer",
		)
	}
}
