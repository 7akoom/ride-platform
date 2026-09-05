package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyOTPLogsInExistingIdentityByEmail(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		9,
		0,
		0,
		0,
		time.UTC,
	)

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "existing@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:         "otp_ch_email_existing",
			Identifier: identifier,
			Purpose:    OTPPurposeLogin,
			CodeHash:   "hashed-code",
			ExpiresAt:  now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_existing",
			IsActive: true,
		},
		findFound: true,
	}

	linkStore := &testIdentifierLinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{
		result: TokenPair{
			AccessToken:                 "access-existing",
			RefreshToken:                "refresh-existing",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		linkStore,
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	sessionMetadata := SessionMetadata{
		ClientID:   "mobile-app",
		DeviceID:   "device-123",
		DeviceName: "iPhone 15 Pro",
		Platform:   "ios",
		AppVersion: "1.0.0",
		IPAddress:  "192.0.2.10",
		UserAgent:  "ride-app/1.0.0",
	}

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_email_existing",
			Code:            "123456",
			SessionMetadata: sessionMetadata,
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if identityRepository.findCalls != 1 {
		t.Fatalf(
			"FindIdentityByIdentifier() calls = %d, expected 1",
			identityRepository.findCalls,
		)
	}

	if identityRepository.findIdentifier != identifier {
		t.Fatalf(
			"searched identifier = %+v, expected %+v",
			identityRepository.findIdentifier,
			identifier,
		)
	}

	if identityRepository.createCalls != 0 {
		t.Fatal(
			"existing identity caused CreateIdentityWithIdentifier()",
		)
	}

	if tokenIssuer.calls != 1 {
		t.Fatalf(
			"TokenIssuer calls = %d, expected 1",
			tokenIssuer.calls,
		)
	}

	if tokenIssuer.input.Identity.ID != "identity_existing" {
		t.Fatalf(
			"issued identity ID = %q, expected %q",
			tokenIssuer.input.Identity.ID,
			"identity_existing",
		)
	}

	if tokenIssuer.input.ChallengeID !=
		"otp_ch_email_existing" {
		t.Fatalf(
			"issued challenge ID = %q, expected %q",
			tokenIssuer.input.ChallengeID,
			"otp_ch_email_existing",
		)
	}

	if tokenIssuer.input.SessionMetadata != sessionMetadata {
		t.Fatalf(
			"issued session metadata = %+v, expected %+v",
			tokenIssuer.input.SessionMetadata,
			sessionMetadata,
		)
	}

	if linkStore.calls != 0 {
		t.Fatal(
			"login invoked identifier link completion store",
		)
	}

	if result.IdentityID != "identity_existing" {
		t.Fatalf(
			"result identity ID = %q, expected %q",
			result.IdentityID,
			"identity_existing",
		)
	}

	if result.AccessToken != "access-existing" {
		t.Fatalf(
			"access token = %q, expected %q",
			result.AccessToken,
			"access-existing",
		)
	}

	if result.RefreshToken != "refresh-existing" {
		t.Fatalf(
			"refresh token = %q, expected %q",
			result.RefreshToken,
			"refresh-existing",
		)
	}
}

func TestVerifyOTPCreatesIdentityForUnknownEmail(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		9,
		30,
		0,
		0,
		time.UTC,
	)

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "new@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:         "otp_ch_email_new",
			Identifier: identifier,
			Purpose:    OTPPurposeLogin,
			CodeHash:   "hashed-code",
			ExpiresAt:  now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository := &testIdentityIdentifierRepository{
		findFound: false,
		createResult: Identity{
			ID:       "identity_new",
			IsActive: true,
		},
	}

	tokenIssuer := &testTokenIssuer{
		result: TokenPair{
			AccessToken:                 "access-new",
			RefreshToken:                "refresh-new",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_email_new",
			Code:            "123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if identityRepository.findCalls != 1 {
		t.Fatalf(
			"FindIdentityByIdentifier() calls = %d, expected 1",
			identityRepository.findCalls,
		)
	}

	if identityRepository.createCalls != 1 {
		t.Fatalf(
			"CreateIdentityWithIdentifier() calls = %d, expected 1",
			identityRepository.createCalls,
		)
	}

	if identityRepository.createIdentifier != identifier {
		t.Fatalf(
			"created identifier = %+v, expected %+v",
			identityRepository.createIdentifier,
			identifier,
		)
	}

	if !identityRepository.createVerifiedAt.Equal(now) {
		t.Fatalf(
			"identifier verified at %v, expected %v",
			identityRepository.createVerifiedAt,
			now,
		)
	}

	if tokenIssuer.calls != 1 {
		t.Fatalf(
			"TokenIssuer calls = %d, expected 1",
			tokenIssuer.calls,
		)
	}

	if tokenIssuer.input.Identity.ID != "identity_new" {
		t.Fatalf(
			"issued identity ID = %q, expected %q",
			tokenIssuer.input.Identity.ID,
			"identity_new",
		)
	}

	if result.IdentityID != "identity_new" {
		t.Fatalf(
			"result identity ID = %q, expected %q",
			result.IdentityID,
			"identity_new",
		)
	}
}

func TestVerifyOTPRejectsInactiveIdentifierIdentityBeforeIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "inactive@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:         "otp_ch_email_inactive",
			Identifier: identifier,
			Purpose:    OTPPurposeLogin,
			CodeHash:   "hashed-code",
			ExpiresAt:  now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_inactive",
			IsActive: false,
		},
		findFound: true,
	}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_email_inactive",
			Code:            "123456",
		},
	)

	if !errors.Is(err, ErrIdentityInactive) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrIdentityInactive,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"inactive identity caused token issuance",
		)
	}
}
