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

func TestRequestOTPUsesGenericPhoneLoginIdentifier(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies.challengeRepository = challengeRepository
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter
	dependencies.clock = &testClock{
		now: time.Date(
			2026,
			time.August,
			13,
			8,
			0,
			0,
			0,
			time.UTC,
		),
	}

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.RequestOTP(
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

	expectedIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647501234567",
	}

	if challengeRepository.createdChallenge.Identifier !=
		expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challengeRepository.createdChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if challengeRepository.createdChallenge.Purpose !=
		OTPPurposeLogin {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challengeRepository.createdChallenge.Purpose,
			OTPPurposeLogin,
		)
	}

	if challengeRepository.createdChallenge.TargetIdentityID != nil {
		t.Fatal(
			"login challenge unexpectedly has target identity",
		)
	}

	if rateLimiter.identifierValue !=
		expectedIdentifier.Value {
		t.Fatalf(
			"rate limiter identifier = %q, expected %q",
			rateLimiter.identifierValue,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.recipient != expectedIdentifier {
		t.Fatalf(
			"OTP delivery recipient = %+v, expected %+v",
			otpDelivery.recipient,
			expectedIdentifier,
		)
	}

	if otpDelivery.code != "123456" {
		t.Fatalf(
			"OTP delivery code = %q, expected %q",
			otpDelivery.code,
			"123456",
		)
	}
}

func TestRequestOTPUsesNormalizedEmailLoginIdentifier(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies.challengeRepository = challengeRepository
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "  User.Name@EXAMPLE.COM  ",
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

	expectedIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "user.name@example.com",
	}

	if challengeRepository.createdChallenge.Identifier !=
		expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challengeRepository.createdChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if challengeRepository.createdChallenge.Purpose !=
		OTPPurposeLogin {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challengeRepository.createdChallenge.Purpose,
			OTPPurposeLogin,
		)
	}

	if challengeRepository.createdChallenge.TargetIdentityID != nil {
		t.Fatal(
			"email login challenge unexpectedly has target identity",
		)
	}

	if rateLimiter.identifierValue !=
		expectedIdentifier.Value {
		t.Fatalf(
			"rate limiter identifier = %q, expected %q",
			rateLimiter.identifierValue,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.recipient != expectedIdentifier {
		t.Fatalf(
			"OTP delivery recipient = %+v, expected %+v",
			otpDelivery.recipient,
			expectedIdentifier,
		)
	}
}

func TestRequestOTPUsesLinkIdentifierScope(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}

	dependencies.challengeRepository =
		challengeRepository

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	targetIdentityID :=
		"  11111111-1111-1111-1111-111111111111  "

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "Link.Me@EXAMPLE.COM",
			},
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &targetIdentityID,
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	challenge :=
		challengeRepository.createdChallenge

	expectedIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "link.me@example.com",
	}

	if challenge.Identifier != expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challenge.Identifier,
			expectedIdentifier,
		)
	}

	if challenge.Purpose !=
		OTPPurposeLinkIdentifier {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challenge.Purpose,
			OTPPurposeLinkIdentifier,
		)
	}

	if challenge.TargetIdentityID == nil {
		t.Fatal(
			"link identifier challenge has nil target identity",
		)
	}

	expectedIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	if *challenge.TargetIdentityID !=
		expectedIdentityID {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			*challenge.TargetIdentityID,
			expectedIdentityID,
		)
	}
}

func TestRequestOTPRejectsInvalidGenericScopeBeforeSideEffects(
	t *testing.T,
) {
	targetIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name  string
		input RequestOTPInput
	}{
		{
			name: "blank generic identifier value",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "   ",
				},
				Purpose: OTPPurposeLogin,
			},
		},
		{
			name: "login cannot target identity",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose:          OTPPurposeLogin,
				TargetIdentityID: &targetIdentityID,
			},
		},
		{
			name: "link identifier requires target identity",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose: OTPPurposeLinkIdentifier,
			},
		},
		{
			name: "generic request requires valid purpose",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
			},
		},
		{
			name: "invalid purpose",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose: OTPPurpose("password_reset"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependencies :=
				newValidServiceConstructorTestDependencies()

			challengeRepository :=
				&testChallengeRepository{}

			otpGenerator := &testOTPGenerator{}
			otpDelivery := &testOTPDelivery{}
			rateLimiter :=
				&testOTPRequestRateLimiter{}

			dependencies.challengeRepository =
				challengeRepository
			dependencies.otpGenerator =
				otpGenerator
			dependencies.otpDelivery =
				otpDelivery
			dependencies.otpRequestRateLimiter =
				rateLimiter

			service :=
				newServiceFromConstructorTestDependencies(
					dependencies,
				)

			_, err := service.RequestOTP(
				context.Background(),
				tt.input,
			)

			if err == nil {
				t.Fatal(
					"RequestOTP() accepted invalid generic OTP scope",
				)
			}

			if rateLimiter.called {
				t.Fatal(
					"rate limiter was called for invalid OTP request",
				)
			}

			if otpGenerator.called {
				t.Fatal(
					"OTP generator was called for invalid OTP request",
				)
			}

			if challengeRepository.createCalled {
				t.Fatal(
					"challenge was created for invalid OTP request",
				)
			}

			if otpDelivery.called {
				t.Fatal(
					"OTP delivery was called for invalid OTP request",
				)
			}
		})
	}
}
