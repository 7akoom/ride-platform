package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRefreshTokenDoesNotRotateWhenAccessTokenSigningFails(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	refreshStore := &testRefreshTokenRotationStore{
		inspectResult: RefreshTokenContext{
			IdentityID:       "identity-123",
			SessionID:        "session-123",
			SessionExpiresAt: now.Add(24 * time.Hour),
		},
	}

	refreshGenerator := &testRefreshTokenGenerator{
		token: "rt_replacement",
	}

	refreshHasher := &testRefreshTokenHasher{}

	accessSigner := &testAccessTokenSigner{
		err: errors.New("signing failed"),
	}

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		refreshStore,
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		refreshGenerator,
		refreshHasher,
		accessSigner,
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

	_, err := service.RefreshToken(
		context.Background(),
		RefreshTokenInput{
			RefreshToken: "rt_current",
		},
	)
	if err == nil {
		t.Fatal(
			"RefreshToken() returned nil error, expected signing failure",
		)
	}

	if accessSigner.calls != 1 {
		t.Fatalf(
			"AccessTokenSigner calls = %d, expected 1",
			accessSigner.calls,
		)
	}

	if refreshStore.rotateCalls != 0 {
		t.Fatalf(
			"Rotate() calls = %d, expected 0",
			refreshStore.rotateCalls,
		)
	}

	if refreshGenerator.calls != 1 {
		t.Fatalf(
			"RefreshTokenGenerator calls = %d, expected 1",
			refreshGenerator.calls,
		)
	}
}

func TestRefreshTokenReturnsReuseErrorWhenRotationDetectsReuse(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	refreshStore := &testRefreshTokenRotationStore{
		inspectResult: RefreshTokenContext{
			IdentityID:       "identity-123",
			SessionID:        "session-123",
			SessionExpiresAt: now.Add(24 * time.Hour),
		},
		rotateErr: ErrRefreshTokenReused,
	}

	refreshGenerator := &testRefreshTokenGenerator{
		token: "rt_replacement",
	}

	refreshHasher := &testRefreshTokenHasher{}

	accessSigner := &testAccessTokenSigner{
		accessToken:      "new-access-token",
		expiresInSeconds: 900,
	}

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		refreshStore,
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		refreshGenerator,
		refreshHasher,
		accessSigner,
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

	result, err := service.RefreshToken(
		context.Background(),
		RefreshTokenInput{
			RefreshToken: "rt_current",
		},
	)

	if !errors.Is(
		err,
		ErrRefreshTokenReused,
	) {
		t.Fatalf(
			"RefreshToken() error = %v, expected %v",
			err,
			ErrRefreshTokenReused,
		)
	}

	if result != (RefreshTokenResult{}) {
		t.Fatalf(
			"RefreshToken() result = %+v, expected empty result",
			result,
		)
	}

	if refreshStore.inspectCalls != 1 {
		t.Fatalf(
			"Inspect() calls = %d, expected 1",
			refreshStore.inspectCalls,
		)
	}

	if refreshStore.rotateCalls != 1 {
		t.Fatalf(
			"Rotate() calls = %d, expected 1",
			refreshStore.rotateCalls,
		)
	}

	if accessSigner.calls != 1 {
		t.Fatalf(
			"AccessTokenSigner calls = %d, expected 1",
			accessSigner.calls,
		)
	}
}
