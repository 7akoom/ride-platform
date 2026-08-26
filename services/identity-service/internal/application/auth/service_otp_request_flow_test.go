package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestOTPStopsBeforeGeneratingOTPWhenRateLimited(
	t *testing.T,
) {
	challengeRepository := &testChallengeRepository{}
	otpGenerator := &testOTPGenerator{}
	otpHasher := &testOTPHasher{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{
		err: ErrOTPRequestRateLimited,
	}
	challengeIDGenerator := &testChallengeIDGenerator{}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		otpGenerator,
		otpHasher,
		otpDelivery,
		rateLimiter,
		challengeIDGenerator,
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

	if !errors.Is(
		err,
		ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"RequestOTP() returned %v, expected %v",
			err,
			ErrOTPRequestRateLimited,
		)
	}

	if !rateLimiter.called {
		t.Fatal("OTP request rate limiter was not called")
	}

	if otpGenerator.called {
		t.Fatal(
			"OTP generator was called after request was rate limited",
		)
	}

	if otpHasher.hashCalled {
		t.Fatal(
			"OTP hasher was called after request was rate limited",
		)
	}

	if challengeIDGenerator.called {
		t.Fatal(
			"challenge ID generator was called after request was rate limited",
		)
	}

	if challengeRepository.createCalled {
		t.Fatal(
			"OTP challenge was created after request was rate limited",
		)
	}

	if otpDelivery.called {
		t.Fatal(
			"OTP delivery was called after request was rate limited",
		)
	}
}

func TestRequestOTPContinuesWhenRateLimiterAllows(
	t *testing.T,
) {
	challengeRepository := &testChallengeRepository{}
	otpGenerator := &testOTPGenerator{}
	otpHasher := &testOTPHasher{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}
	challengeIDGenerator := &testChallengeIDGenerator{}

	fixedTime := time.Date(
		2026,
		time.August,
		10,
		6,
		0,
		0,
		0,
		time.UTC,
	)

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		otpGenerator,
		otpHasher,
		otpDelivery,
		rateLimiter,
		challengeIDGenerator,
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
			now: fixedTime,
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	result, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "  +9647501234567  ",
			},
			Purpose: OTPPurposeLogin,
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	if !rateLimiter.called {
		t.Fatal(
			"OTP request rate limiter was not called",
		)
	}

	if !otpGenerator.called {
		t.Fatal(
			"OTP generator was not called",
		)
	}

	if !otpHasher.hashCalled {
		t.Fatal(
			"OTP hasher was not called",
		)
	}

	if otpHasher.hashChallengeID != "otp_ch_test" {
		t.Fatalf(
			"OTP hasher challenge ID = %q, expected %q",
			otpHasher.hashChallengeID,
			"otp_ch_test",
		)
	}

	if otpHasher.hashCode != "123456" {
		t.Fatalf(
			"OTP hasher code = %q, expected %q",
			otpHasher.hashCode,
			"123456",
		)
	}

	if !challengeIDGenerator.called {
		t.Fatal(
			"challenge ID generator was not called",
		)
	}

	if !challengeRepository.createCalled {
		t.Fatal(
			"OTP challenge was not created",
		)
	}

	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not called",
		)
	}

	if result.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"ChallengeID is %q, expected %q",
			result.ChallengeID,
			"otp_ch_test",
		)
	}

	if result.ExpiresInSeconds != 300 {
		t.Fatalf(
			"ExpiresInSeconds is %d, expected 300",
			result.ExpiresInSeconds,
		)
	}
}
