package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRefreshTokenRejectsBlankRefreshToken(
	t *testing.T,
) {
	testCases := []struct {
		name         string
		refreshToken string
	}{
		{
			name:         "empty",
			refreshToken: "",
		},
		{
			name:         "spaces",
			refreshToken: "   ",
		},
		{
			name:         "tabs and newlines",
			refreshToken: "\t\n ",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			refreshStore := &testRefreshTokenRotationStore{}
			refreshHasher := &testRefreshTokenHasher{}
			refreshGenerator := &testRefreshTokenGenerator{}
			accessSigner := &testAccessTokenSigner{}

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
				&testClock{},
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
					RefreshToken: testCase.refreshToken,
				},
			)

			if !errors.Is(
				err,
				ErrInvalidRefreshToken,
			) {
				t.Fatalf(
					"RefreshToken() error = %v, expected %v",
					err,
					ErrInvalidRefreshToken,
				)
			}

			if refreshHasher.calls != 0 {
				t.Fatalf(
					"RefreshTokenHasher calls = %d, expected 0",
					refreshHasher.calls,
				)
			}

			if refreshStore.inspectCalls != 0 {
				t.Fatalf(
					"RefreshTokenRotationStore Inspect calls = %d, expected 0",
					refreshStore.inspectCalls,
				)
			}

			if refreshGenerator.calls != 0 {
				t.Fatalf(
					"RefreshTokenGenerator calls = %d, expected 0",
					refreshGenerator.calls,
				)
			}

			if accessSigner.calls != 0 {
				t.Fatalf(
					"AccessTokenSigner calls = %d, expected 0",
					accessSigner.calls,
				)
			}
		})
	}
}

func TestRefreshTokenRotatesTokenAndClampsExpirationToSession(
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

	sessionExpiresAt := now.Add(
		2 * time.Hour,
	)

	refreshStore := &testRefreshTokenRotationStore{
		inspectResult: RefreshTokenContext{
			IdentityID:       "identity-123",
			SessionID:        "session-123",
			SessionExpiresAt: sessionExpiresAt,
		},
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
	if err != nil {
		t.Fatalf(
			"RefreshToken() returned an error: %v",
			err,
		)
	}

	if result.IdentityID != "identity-123" {
		t.Fatalf(
			"IdentityID = %q, expected %q",
			result.IdentityID,
			"identity-123",
		)
	}

	if result.AccessToken != "new-access-token" {
		t.Fatalf(
			"AccessToken = %q, expected %q",
			result.AccessToken,
			"new-access-token",
		)
	}

	if result.RefreshToken != "rt_replacement" {
		t.Fatalf(
			"RefreshToken = %q, expected %q",
			result.RefreshToken,
			"rt_replacement",
		)
	}

	if result.AccessTokenExpiresInSeconds != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds = %d, expected 900",
			result.AccessTokenExpiresInSeconds,
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

	if refreshStore.rotationInput.CurrentTokenHash !=
		"hashed_rt_current" {
		t.Fatalf(
			"CurrentTokenHash = %q, expected %q",
			refreshStore.rotationInput.CurrentTokenHash,
			"hashed_rt_current",
		)
	}

	if refreshStore.rotationInput.ReplacementTokenHash !=
		"hashed_rt_replacement" {
		t.Fatalf(
			"ReplacementTokenHash = %q, expected %q",
			refreshStore.rotationInput.ReplacementTokenHash,
			"hashed_rt_replacement",
		)
	}

	if !refreshStore.rotationInput.ReplacementExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Fatalf(
			"ReplacementExpiresAt = %v, expected %v",
			refreshStore.rotationInput.ReplacementExpiresAt,
			sessionExpiresAt,
		)
	}

	if accessSigner.calls != 1 {
		t.Fatalf(
			"AccessTokenSigner calls = %d, expected 1",
			accessSigner.calls,
		)
	}

	if accessSigner.identityID != "identity-123" {
		t.Fatalf(
			"signed IdentityID = %q, expected %q",
			accessSigner.identityID,
			"identity-123",
		)
	}

	if accessSigner.sessionID != "session-123" {
		t.Fatalf(
			"signed SessionID = %q, expected %q",
			accessSigner.sessionID,
			"session-123",
		)
	}

	if !accessSigner.issuedAt.Equal(now) {
		t.Fatalf(
			"signed issuedAt = %v, expected %v",
			accessSigner.issuedAt,
			now,
		)
	}

	if !accessSigner.sessionExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Fatalf(
			"signed sessionExpiresAt = %v, expected %v",
			accessSigner.sessionExpiresAt,
			sessionExpiresAt,
		)
	}

	if refreshHasher.calls != 2 {
		t.Fatalf(
			"RefreshTokenHasher calls = %d, expected 2",
			refreshHasher.calls,
		)
	}
}

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
