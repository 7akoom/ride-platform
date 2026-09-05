package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRevokeSessionRecordsSuccessMetric(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}

	service := &service{
		sessionManagementRevocationStore: &testSessionManagementRevocationStore{},
		metricsRecorder:                  metricsRecorder,
		clock: &testClock{
			now: sessionMetricsTestTime(),
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)
	if err != nil {
		t.Fatalf(
			"RevokeSession() returned an error: %v",
			err,
		)
	}

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationRevoke,
		MetricOutcomeSuccess,
	)
}

func TestRevokeSessionRecordsRejectedMetricWhenSessionNotFound(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}

	service := &service{
		sessionManagementRevocationStore: &testSessionManagementRevocationStore{
			err: ErrSessionNotFound,
		},
		metricsRecorder: metricsRecorder,
		clock: &testClock{
			now: sessionMetricsTestTime(),
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: "identity-123",
			SessionID:  "session-missing",
		},
	)
	if !errors.Is(
		err,
		ErrSessionNotFound,
	) {
		t.Fatalf(
			"RevokeSession() error = %v, expected %v",
			err,
			ErrSessionNotFound,
		)
	}

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationRevoke,
		MetricOutcomeRejected,
	)
}

func TestRevokeSessionRecordsFailedMetricForUnexpectedStoreError(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}
	storeErr := errors.New(
		"session revocation store failed",
	)

	service := &service{
		sessionManagementRevocationStore: &testSessionManagementRevocationStore{
			err: storeErr,
		},
		metricsRecorder: metricsRecorder,
		clock: &testClock{
			now: sessionMetricsTestTime(),
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)
	if !errors.Is(
		err,
		storeErr,
	) {
		t.Fatalf(
			"RevokeSession() error = %v, expected wrapped %v",
			err,
			storeErr,
		)
	}

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationRevoke,
		MetricOutcomeFailed,
	)
}

func TestRevokeSessionDoesNotRecordSessionMetricForInvalidInput(
	t *testing.T,
) {
	metricsRecorder := &testMetricsRecorder{}
	revocationStore := &testSessionManagementRevocationStore{}

	service := &service{
		sessionManagementRevocationStore: revocationStore,
		metricsRecorder:                  metricsRecorder,
		clock: &testClock{
			now: sessionMetricsTestTime(),
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: "identity-123",
			SessionID:  "   ",
		},
	)
	if !errors.Is(
		err,
		ErrSessionNotFound,
	) {
		t.Fatalf(
			"RevokeSession() error = %v, expected %v",
			err,
			ErrSessionNotFound,
		)
	}

	if revocationStore.called {
		t.Fatal(
			"RevokeSession() called store for invalid input",
		)
	}

	if len(metricsRecorder.sessionOperations) != 0 {
		t.Fatalf(
			"session operation metric count = %d, expected 0",
			len(metricsRecorder.sessionOperations),
		)
	}
}

func sessionMetricsTestTime() time.Time {
	return time.Date(
		2026,
		time.August,
		26,
		13,
		0,
		0,
		0,
		time.UTC,
	)
}
