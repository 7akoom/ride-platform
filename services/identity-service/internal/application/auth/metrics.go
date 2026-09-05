package auth

import (
	"context"
	"time"
)

type MetricOutcome string

const (
	MetricOutcomeSuccess  MetricOutcome = "success"
	MetricOutcomeRejected MetricOutcome = "rejected"
	MetricOutcomeFailed   MetricOutcome = "failed"
)

type AuthMetricOperation string

const (
	AuthMetricOperationLogin             AuthMetricOperation = "login"
	AuthMetricOperationRefresh           AuthMetricOperation = "refresh"
	AuthMetricOperationLogout            AuthMetricOperation = "logout"
	AuthMetricOperationAccessTokenVerify AuthMetricOperation = "access_token_verify"
)

type SessionMetricOperation string

const (
	SessionMetricOperationCreate    SessionMetricOperation = "create"
	SessionMetricOperationRefresh   SessionMetricOperation = "refresh"
	SessionMetricOperationRevoke    SessionMetricOperation = "revoke"
	SessionMetricOperationRevokeAll SessionMetricOperation = "revoke_all"
)

type SecurityMetricEvent string

const (
	SecurityMetricEventRefreshTokenReuse   SecurityMetricEvent = "refresh_token_reuse"
	SecurityMetricEventOTPRateLimited      SecurityMetricEvent = "otp_rate_limited"
	SecurityMetricEventOTPAttemptsExceeded SecurityMetricEvent = "otp_attempts_exceeded"
	SecurityMetricEventChallengeReplay     SecurityMetricEvent = "challenge_replay"
)

type MetricsRecorder interface {
	RecordAuthOperation(
		ctx context.Context,
		operation AuthMetricOperation,
		outcome MetricOutcome,
		duration time.Duration,
	)

	RecordOTPRequest(
		ctx context.Context,
		purpose OTPPurpose,
		channel OTPDeliveryChannel,
		outcome MetricOutcome,
	)

	RecordOTPVerification(
		ctx context.Context,
		purpose OTPPurpose,
		outcome MetricOutcome,
	)

	RecordSessionOperation(
		ctx context.Context,
		operation SessionMetricOperation,
		outcome MetricOutcome,
	)

	RecordSecurityEvent(
		ctx context.Context,
		event SecurityMetricEvent,
	)
}

type noopMetricsRecorder struct{}

func newNoopMetricsRecorder() MetricsRecorder {
	return noopMetricsRecorder{}
}

func (noopMetricsRecorder) RecordAuthOperation(
	context.Context,
	AuthMetricOperation,
	MetricOutcome,
	time.Duration,
) {
}

func (noopMetricsRecorder) RecordOTPRequest(
	context.Context,
	OTPPurpose,
	OTPDeliveryChannel,
	MetricOutcome,
) {
}

func (noopMetricsRecorder) RecordOTPVerification(
	context.Context,
	OTPPurpose,
	MetricOutcome,
) {
}

func (noopMetricsRecorder) RecordSessionOperation(
	context.Context,
	SessionMetricOperation,
	MetricOutcome,
) {
}

func (noopMetricsRecorder) RecordSecurityEvent(
	context.Context,
	SecurityMetricEvent,
) {
}
