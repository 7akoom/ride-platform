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
			metricsRecorder := &testMetricsRecorder{}

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
				WithMetricsRecorder(metricsRecorder),
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
			requireSingleAuthOperationMetric(
				t,
				metricsRecorder,
				AuthMetricOperationRefresh,
				MetricOutcomeRejected,
			)

			if len(metricsRecorder.sessionOperations) != 0 {
				t.Fatalf(
					"session operation metric count = %d, expected 0",
					len(metricsRecorder.sessionOperations),
				)
			}

			if len(metricsRecorder.securityEvents) != 0 {
				t.Fatalf(
					"security event metric count = %d, expected 0",
					len(metricsRecorder.securityEvents),
				)
			}
		})
	}
}
