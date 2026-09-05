package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyOTPMapsConcurrentCancellationFromRecordFailedAttempt(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_test",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose:        OTPPurposeLogin,
			CodeHash:       "hashed-code",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
		recordFailedAttemptErr: ErrChallengeCancelled,
	}

	otpHasher := &testOTPHasher{
		compareMatchesSet: true,
		compareMatches:    false,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		otpHasher,
		&testOTPDelivery{},
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
			now: now,
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_test",
			Code:            "000000",
		},
	)

	if !errors.Is(
		err,
		ErrChallengeCancelled,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeCancelled,
		)
	}

	if !otpHasher.compareCalled {
		t.Fatal(
			"OTP hasher Compare() was not called",
		)
	}

	if otpHasher.compareHash != "hashed-code" {
		t.Fatalf(
			"OTP hasher comparison hash = %q, expected %q",
			otpHasher.compareHash,
			"hashed-code",
		)
	}

	if otpHasher.compareChallengeID != "otp_ch_test" {
		t.Fatalf(
			"OTP hasher comparison challenge ID = %q, expected %q",
			otpHasher.compareChallengeID,
			"otp_ch_test",
		)
	}

	if otpHasher.compareCode != "000000" {
		t.Fatalf(
			"OTP hasher comparison code = %q, expected %q",
			otpHasher.compareCode,
			"000000",
		)
	}

	if !challengeRepository.recordFailedAttemptCalled {
		t.Fatal(
			"RecordFailedAttempt() was not called",
		)
	}

	if challengeRepository.markVerifiedCalled {
		t.Fatal(
			"MarkVerified() was called after concurrent cancellation",
		)
	}
}

func TestVerifyOTPDoesNotRecordFailedAttemptWhenHasherFails(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		12,
		7,
		0,
		0,
		0,
		time.UTC,
	)

	compareError := errors.New(
		"corrupted OTP hash",
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_test",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose:        OTPPurposeLogin,
			CodeHash:       "corrupted-hash",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	otpHasher := &testOTPHasher{
		compareErr: compareError,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		otpHasher,
		&testOTPDelivery{},
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
			now: now,
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_test",
			Code:            "123456",
		},
	)

	if err == nil {
		t.Fatal(
			"VerifyOTP() returned nil error when OTP hasher failed",
		)
	}

	if !errors.Is(err, compareError) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected wrapped hasher error",
			err,
		)
	}

	if challengeRepository.recordFailedAttemptCalled {
		t.Fatal(
			"RecordFailedAttempt() was called when OTP hasher failed",
		)
	}
}

func TestVerifyOTPMapsConcurrentCancellationFromTokenIssuer(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_test",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose:        OTPPurposeLogin,
			CodeHash:       "hashed-code",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identity := Identity{
		ID:       "identity-123",
		IsActive: true,
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: identity,
		findFound:  true,
	}

	tokenIssuer := &testTokenIssuer{
		err: ErrChallengeCancelled,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		tokenIssuer,
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: now,
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_test",
			Code:            "123456",
		},
	)

	if !errors.Is(
		err,
		ErrChallengeCancelled,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeCancelled,
		)
	}

	if tokenIssuer.calls != 1 {
		t.Fatalf(
			"TokenIssuer.Issue() calls = %d, expected 1",
			tokenIssuer.calls,
		)
	}

	if tokenIssuer.input.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"TokenIssuer.Issue() challenge ID = %q, expected %q",
			tokenIssuer.input.ChallengeID,
			"otp_ch_test",
		)
	}

	if !tokenIssuer.input.VerifiedAt.Equal(now) {
		t.Fatalf(
			"TokenIssuer.Issue() verification time = %v, expected %v",
			tokenIssuer.input.VerifiedAt,
			now,
		)
	}

	if tokenIssuer.input.Identity.ID != identity.ID {
		t.Fatalf(
			"TokenIssuer.Issue() identity ID = %q, expected %q",
			tokenIssuer.input.Identity.ID,
			identity.ID,
		)
	}

	if challengeRepository.markVerifiedCalled {
		t.Fatal(
			"MarkVerified() was called outside atomic token issuance",
		)
	}

	if challengeRepository.recordFailedAttemptCalled {
		t.Fatal(
			"RecordFailedAttempt() was called for a valid OTP",
		)
	}
}
