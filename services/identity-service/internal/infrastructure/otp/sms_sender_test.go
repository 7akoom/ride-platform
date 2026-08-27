package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type testSMSProvider struct {
	calls   int
	message SMSMessage
	err     error
}

func (p *testSMSProvider) Send(
	ctx context.Context,
	message SMSMessage,
) error {
	p.calls++
	p.message = message

	return p.err
}

type testSMSMessageRenderer struct {
	calls int
	input OTPMessageRenderInput
	body  string
	err   error
}

func (r *testSMSMessageRenderer) Render(
	input OTPMessageRenderInput,
) (string, error) {
	r.calls++
	r.input = input

	return r.body, r.err
}

func TestProviderSMSSenderRendersAndSendsMessage(
	t *testing.T,
) {
	provider := &testSMSProvider{}

	renderer := &testSMSMessageRenderer{
		body: "Your verification code is 123456",
	}

	sender, err := NewProviderSMSSender(
		provider,
		renderer,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderSMSSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		" +9647501234567 ",
		" 123456 ",
		auth.OTPPurposeLogin,
		"ar",
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

	expectedRenderInput := OTPMessageRenderInput{
		Code:    "123456",
		Purpose: auth.OTPPurposeLogin,
		Locale:  "ar",
	}

	if renderer.input != expectedRenderInput {
		t.Fatalf(
			"renderer input = %+v, expected %+v",
			renderer.input,
			expectedRenderInput,
		)
	}

	if provider.calls != 1 {
		t.Fatalf(
			"provider calls = %d, expected 1",
			provider.calls,
		)
	}

	expectedMessage := SMSMessage{
		To:   "+9647501234567",
		Body: "Your verification code is 123456",
	}

	if provider.message != expectedMessage {
		t.Fatalf(
			"provider message = %+v, expected %+v",
			provider.message,
			expectedMessage,
		)
	}
}

func TestProviderSMSSenderPropagatesProviderError(
	t *testing.T,
) {
	expectedErr := errors.New(
		"provider unavailable",
	)

	provider := &testSMSProvider{
		err: expectedErr,
	}

	renderer := &testSMSMessageRenderer{
		body: "Your verification code is 123456",
	}

	sender, err := NewProviderSMSSender(
		provider,
		renderer,
	)
	if err != nil {
		t.Fatalf(
			"NewProviderSMSSender() returned an error: %v",
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

func TestNewProviderSMSSenderRequiresDependencies(
	t *testing.T,
) {
	tests := []struct {
		name     string
		provider SMSProvider
		renderer SMSMessageRenderer
	}{
		{
			name:     "missing provider",
			provider: nil,
			renderer: &testSMSMessageRenderer{},
		},
		{
			name:     "missing renderer",
			provider: &testSMSProvider{},
			renderer: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			sender, err := NewProviderSMSSender(
				testCase.provider,
				testCase.renderer,
			)

			if err == nil {
				t.Fatal(
					"NewProviderSMSSender() accepted missing dependency",
				)
			}

			if sender != nil {
				t.Fatal(
					"NewProviderSMSSender() returned sender with missing dependency",
				)
			}
		})
	}
}
