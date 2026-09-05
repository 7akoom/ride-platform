package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLogoutHashesRefreshTokenAndRevokesSession(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		11,
		30,
		0,
		0,
		time.UTC,
	)

	sessionRevocationStore :=
		&testSessionRevocationStore{}

	refreshHasher :=
		&testRefreshTokenHasher{}

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
		&testRefreshTokenRotationStore{},
		sessionRevocationStore,
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		refreshHasher,
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

	err := service.Logout(
		context.Background(),
		LogoutInput{
			RefreshToken: "rt_current",
		},
	)
	if err != nil {
		t.Fatalf(
			"Logout() returned an error: %v",
			err,
		)
	}

	if refreshHasher.calls != 1 {
		t.Fatalf(
			"RefreshTokenHasher calls = %d, expected 1",
			refreshHasher.calls,
		)
	}

	if len(refreshHasher.inputs) != 1 {
		t.Fatalf(
			"RefreshTokenHasher inputs = %d, expected 1",
			len(refreshHasher.inputs),
		)
	}

	if refreshHasher.inputs[0] != "rt_current" {
		t.Fatalf(
			"hashed refresh token input = %q, expected %q",
			refreshHasher.inputs[0],
			"rt_current",
		)
	}

	if sessionRevocationStore.calls != 1 {
		t.Fatalf(
			"SessionRevocationStore calls = %d, expected 1",
			sessionRevocationStore.calls,
		)
	}

	if sessionRevocationStore.refreshTokenHash !=
		"hashed_rt_current" {
		t.Fatalf(
			"refresh token hash = %q, expected %q",
			sessionRevocationStore.refreshTokenHash,
			"hashed_rt_current",
		)
	}

	if !sessionRevocationStore.revokedAt.Equal(now) {
		t.Fatalf(
			"revokedAt = %v, expected %v",
			sessionRevocationStore.revokedAt,
			now,
		)
	}
}

func TestLogoutRejectsBlankRefreshToken(
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
			sessionRevocationStore :=
				&testSessionRevocationStore{}

			refreshHasher :=
				&testRefreshTokenHasher{}

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
				&testRefreshTokenRotationStore{},
				sessionRevocationStore,
				&testAllSessionsRevocationStore{},
				&testSessionReader{},
				&testSessionManagementRevocationStore{},
				&testRefreshTokenGenerator{},
				refreshHasher,
				&testAccessTokenSigner{},
				&testClock{},
				5*time.Minute,
				OTPRequestRateLimitPolicy{
					Cooldown:    time.Minute,
					Window:      15 * time.Minute,
					MaxRequests: 5,
				},
				29*24*time.Hour,
			)

			err := service.Logout(
				context.Background(),
				LogoutInput{
					RefreshToken: testCase.refreshToken,
				},
			)

			if !errors.Is(
				err,
				ErrInvalidRefreshToken,
			) {
				t.Fatalf(
					"Logout() error = %v, expected %v",
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

			if sessionRevocationStore.calls != 0 {
				t.Fatalf(
					"SessionRevocationStore calls = %d, expected 0",
					sessionRevocationStore.calls,
				)
			}
		})
	}
}

func TestLogoutAllSessionsHashesRefreshTokenAndRevokesAllSessions(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		12,
		30,
		0,
		0,
		time.UTC,
	)

	allSessionsRevocationStore :=
		&testAllSessionsRevocationStore{}

	refreshHasher :=
		&testRefreshTokenHasher{}

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
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		allSessionsRevocationStore,
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		refreshHasher,
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

	err := service.LogoutAllSessions(
		context.Background(),
		LogoutAllSessionsInput{
			RefreshToken: "rt_current",
		},
	)
	if err != nil {
		t.Fatalf(
			"LogoutAllSessions() returned an error: %v",
			err,
		)
	}

	if refreshHasher.calls != 1 {
		t.Fatalf(
			"RefreshTokenHasher calls = %d, expected 1",
			refreshHasher.calls,
		)
	}

	if len(refreshHasher.inputs) != 1 {
		t.Fatalf(
			"RefreshTokenHasher inputs = %d, expected 1",
			len(refreshHasher.inputs),
		)
	}

	if refreshHasher.inputs[0] != "rt_current" {
		t.Fatalf(
			"hashed refresh token input = %q, expected %q",
			refreshHasher.inputs[0],
			"rt_current",
		)
	}

	if allSessionsRevocationStore.calls != 1 {
		t.Fatalf(
			"AllSessionsRevocationStore calls = %d, expected 1",
			allSessionsRevocationStore.calls,
		)
	}

	if allSessionsRevocationStore.refreshTokenHash !=
		"hashed_rt_current" {
		t.Fatalf(
			"refresh token hash = %q, expected %q",
			allSessionsRevocationStore.refreshTokenHash,
			"hashed_rt_current",
		)
	}

	if !allSessionsRevocationStore.revokedAt.Equal(now) {
		t.Fatalf(
			"revokedAt = %v, expected %v",
			allSessionsRevocationStore.revokedAt,
			now,
		)
	}
}

func TestLogoutAllSessionsRejectsBlankRefreshToken(
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
			allSessionsRevocationStore :=
				&testAllSessionsRevocationStore{}

			refreshHasher :=
				&testRefreshTokenHasher{}

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
				&testRefreshTokenRotationStore{},
				&testSessionRevocationStore{},
				allSessionsRevocationStore,
				&testSessionReader{},
				&testSessionManagementRevocationStore{},
				&testRefreshTokenGenerator{},
				refreshHasher,
				&testAccessTokenSigner{},
				&testClock{},
				5*time.Minute,
				OTPRequestRateLimitPolicy{
					Cooldown:    time.Minute,
					Window:      15 * time.Minute,
					MaxRequests: 5,
				},
				29*24*time.Hour,
			)

			err := service.LogoutAllSessions(
				context.Background(),
				LogoutAllSessionsInput{
					RefreshToken: testCase.refreshToken,
				},
			)

			if !errors.Is(
				err,
				ErrInvalidRefreshToken,
			) {
				t.Fatalf(
					"LogoutAllSessions() error = %v, expected %v",
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

			if allSessionsRevocationStore.calls != 0 {
				t.Fatalf(
					"AllSessionsRevocationStore calls = %d, expected 0",
					allSessionsRevocationStore.calls,
				)
			}
		})
	}
}
