package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestOTPCancelsChallengeWhenDeliveryFails(
	t *testing.T,
) {
	deliveryError := errors.New("SMS provider unavailable")

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{
		err: deliveryError,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		&testOTPHasher{},
		otpDelivery,
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: time.Date(
				2026,
				time.August,
				10,
				6,
				0,
				0,
				0,
				time.UTC,
			),
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
		},
	)

	if err == nil {
		t.Fatal(
			"RequestOTP() returned nil error when delivery failed",
		)
	}

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"RequestOTP() returned %v, expected delivery error",
			err,
		)
	}

	if !challengeRepository.createCalled {
		t.Fatal(
			"OTP challenge was not created before delivery",
		)
	}

	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not attempted",
		)
	}

	if !challengeRepository.cancelCalled {
		t.Fatal(
			"OTP challenge was not cancelled after delivery failure",
		)
	}

	if challengeRepository.cancelledChallengeID != "otp_ch_test" {
		t.Fatalf(
			"cancelled challenge ID is %q, expected %q",
			challengeRepository.cancelledChallengeID,
			"otp_ch_test",
		)
	}
}

func TestRequestOTPCancelsChallengeWithIndependentBoundedContextWhenRequestIsCancelled(
	t *testing.T,
) {
	deliveryError := errors.New(
		"SMS delivery interrupted",
	)

	requestCtx, cancelRequest :=
		context.WithCancel(context.Background())
	defer cancelRequest()

	challengeRepository := &testChallengeRepository{}

	otpDelivery := &testOTPDelivery{
		err: deliveryError,
		onSend: func() {
			cancelRequest()
		},
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		&testOTPHasher{},
		otpDelivery,
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: time.Date(
				2026,
				time.August,
				12,
				12,
				0,
				0,
				0,
				time.UTC,
			),
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	_, err := service.RequestOTP(
		requestCtx,
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
		},
	)

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"RequestOTP() error = %v, expected delivery error",
			err,
		)
	}

	if requestCtx.Err() != context.Canceled {
		t.Fatalf(
			"request context error = %v, expected context canceled",
			requestCtx.Err(),
		)
	}

	if !challengeRepository.cancelCalled {
		t.Fatal(
			"OTP challenge was not cancelled after delivery failure",
		)
	}

	if challengeRepository.cancelContextErr != nil {
		t.Fatalf(
			"Cancel() received cancelled context: %v",
			challengeRepository.cancelContextErr,
		)
	}

	if !challengeRepository.cancelContextHasDeadline {
		t.Fatal(
			"Cancel() compensation context has no deadline",
		)
	}
}

func TestRequestOTPReturnsDeliveryAndCancellationErrors(
	t *testing.T,
) {
	deliveryError := errors.New(
		"SMS provider unavailable",
	)

	cancellationError := errors.New(
		"challenge cancellation failed",
	)

	challengeRepository := &testChallengeRepository{
		cancelErr: cancellationError,
	}

	otpDelivery := &testOTPDelivery{
		err: deliveryError,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		&testOTPHasher{},
		otpDelivery,
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: time.Date(
				2026,
				time.August,
				12,
				12,
				0,
				0,
				0,
				time.UTC,
			),
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
		},
	)

	if err == nil {
		t.Fatal(
			"RequestOTP() returned nil error when delivery and cancellation failed",
		)
	}

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"RequestOTP() error = %v, expected delivery error to be preserved",
			err,
		)
	}

	if !errors.Is(err, cancellationError) {
		t.Fatalf(
			"RequestOTP() error = %v, expected cancellation error to be preserved",
			err,
		)
	}

	if !challengeRepository.cancelCalled {
		t.Fatal(
			"OTP challenge cancellation was not attempted after delivery failure",
		)
	}
}
