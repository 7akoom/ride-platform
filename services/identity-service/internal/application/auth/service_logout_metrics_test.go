package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLogoutRecordsSuccessfulAuthAndSessionMetrics(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}

	service := newLogoutMetricsServiceForTest(
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		metricsRecorder,
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

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogout,
		MetricOutcomeSuccess,
	)

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationRevoke,
		MetricOutcomeSuccess,
	)
}

func TestLogoutRecordsRejectedAuthMetricForBlankRefreshToken(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}

	service := newLogoutMetricsServiceForTest(
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		metricsRecorder,
	)

	err := service.Logout(
		context.Background(),
		LogoutInput{
			RefreshToken: "   ",
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

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogout,
		MetricOutcomeRejected,
	)

	if len(metricsRecorder.sessionOperations) != 0 {
		t.Fatalf(
			"session operation metric count = %d, expected 0",
			len(metricsRecorder.sessionOperations),
		)
	}
}

func TestLogoutRecordsFailedAuthAndSessionMetricsWhenRevocationFails(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}

	service := newLogoutMetricsServiceForTest(
		&testSessionRevocationStore{
			err: errors.New(
				"revocation store failed",
			),
		},
		&testAllSessionsRevocationStore{},
		metricsRecorder,
	)

	err := service.Logout(
		context.Background(),
		LogoutInput{
			RefreshToken: "rt_current",
		},
	)
	if err == nil {
		t.Fatal(
			"Logout() expected an error",
		)
	}

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogout,
		MetricOutcomeFailed,
	)

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationRevoke,
		MetricOutcomeFailed,
	)
}

func TestLogoutAllSessionsRecordsSuccessfulMetrics(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}

	service := newLogoutMetricsServiceForTest(
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		metricsRecorder,
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

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogout,
		MetricOutcomeSuccess,
	)

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationRevokeAll,
		MetricOutcomeSuccess,
	)
}

func newLogoutMetricsServiceForTest(
	sessionRevocationStore SessionRevocationStore,
	allSessionsRevocationStore AllSessionsRevocationStore,
	metricsRecorder MetricsRecorder,
) Service {
	return NewServiceWithIdentityIdentifiers(
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
		allSessionsRevocationStore,
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
		WithMetricsRecorder(
			metricsRecorder,
		),
	)
}

func requireSingleAuthOperationMetric(
	t *testing.T,
	recorder *testMetricsRecorder,
	operation AuthMetricOperation,
	outcome MetricOutcome,
) {
	t.Helper()

	if len(recorder.authOperations) != 1 {
		t.Fatalf(
			"auth operation metric count = %d, expected 1",
			len(recorder.authOperations),
		)
	}

	metricValue := recorder.authOperations[0]

	if metricValue.operation != operation {
		t.Fatalf(
			"auth operation = %q, expected %q",
			metricValue.operation,
			operation,
		)
	}

	if metricValue.outcome != outcome {
		t.Fatalf(
			"auth outcome = %q, expected %q",
			metricValue.outcome,
			outcome,
		)
	}

	if metricValue.duration < 0 {
		t.Fatalf(
			"auth duration = %v, expected non-negative duration",
			metricValue.duration,
		)
	}
}

func requireSingleSessionOperationMetric(
	t *testing.T,
	recorder *testMetricsRecorder,
	operation SessionMetricOperation,
	outcome MetricOutcome,
) {
	t.Helper()

	if len(recorder.sessionOperations) != 1 {
		t.Fatalf(
			"session operation metric count = %d, expected 1",
			len(recorder.sessionOperations),
		)
	}

	metricValue := recorder.sessionOperations[0]

	if metricValue.operation != operation {
		t.Fatalf(
			"session operation = %q, expected %q",
			metricValue.operation,
			operation,
		)
	}

	if metricValue.outcome != outcome {
		t.Fatalf(
			"session outcome = %q, expected %q",
			metricValue.outcome,
			outcome,
		)
	}
}
