package token

import (
	"context"
	"time"
)

type AccessTokenVerificationMetricOutcome string

const (
	AccessTokenVerificationMetricOutcomeSuccess  AccessTokenVerificationMetricOutcome = "success"
	AccessTokenVerificationMetricOutcomeRejected AccessTokenVerificationMetricOutcome = "rejected"
	AccessTokenVerificationMetricOutcomeFailed   AccessTokenVerificationMetricOutcome = "failed"
)

type AccessTokenVerificationMetricsRecorder interface {
	RecordAccessTokenVerification(
		ctx context.Context,
		outcome AccessTokenVerificationMetricOutcome,
		duration time.Duration,
	)
}

type noopAccessTokenVerificationMetricsRecorder struct{}

func (noopAccessTokenVerificationMetricsRecorder) RecordAccessTokenVerification(
	context.Context,
	AccessTokenVerificationMetricOutcome,
	time.Duration,
) {
}

func newNoopAccessTokenVerificationMetricsRecorder() AccessTokenVerificationMetricsRecorder {
	return noopAccessTokenVerificationMetricsRecorder{}
}
