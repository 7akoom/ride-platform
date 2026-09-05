package observability

import (
	"context"
	"errors"
	"time"

	authapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	otpinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	tokeninfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/token"
)

type AuthMetricsRecorder struct {
	metrics *AuthMetrics
}

var _ authapp.MetricsRecorder = (*AuthMetricsRecorder)(nil)
var _ otpinfra.DeliveryMetricsRecorder = (*AuthMetricsRecorder)(nil)
var _ otpinfra.ProviderHealthMetricsRecorder = (*AuthMetricsRecorder)(nil)

var _ tokeninfra.AccessTokenVerificationMetricsRecorder = (*AuthMetricsRecorder)(nil)

func NewAuthMetricsRecorder(
	metrics *AuthMetrics,
) (*AuthMetricsRecorder, error) {
	if metrics == nil {
		return nil, errors.New(
			"authentication metrics are required",
		)
	}

	return &AuthMetricsRecorder{
		metrics: metrics,
	}, nil
}

func (r *AuthMetricsRecorder) RecordAuthOperation(
	ctx context.Context,
	operation authapp.AuthMetricOperation,
	outcome authapp.MetricOutcome,
	duration time.Duration,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordAuthOperation(
		ctx,
		AuthMetricOperation(operation),
		MetricOutcome(outcome),
		duration,
	)
}

func (r *AuthMetricsRecorder) RecordOTPRequest(
	ctx context.Context,
	purpose authapp.OTPPurpose,
	channel authapp.OTPDeliveryChannel,
	outcome authapp.MetricOutcome,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordOTPRequest(
		ctx,
		OTPMetricPurpose(purpose),
		OTPMetricChannel(channel),
		MetricOutcome(outcome),
	)
}

func (r *AuthMetricsRecorder) RecordOTPVerification(
	ctx context.Context,
	purpose authapp.OTPPurpose,
	outcome authapp.MetricOutcome,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordOTPVerification(
		ctx,
		OTPMetricPurpose(purpose),
		MetricOutcome(outcome),
	)
}

func (r *AuthMetricsRecorder) RecordSessionOperation(
	ctx context.Context,
	operation authapp.SessionMetricOperation,
	outcome authapp.MetricOutcome,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordSessionOperation(
		ctx,
		SessionMetricOperation(operation),
		MetricOutcome(outcome),
	)
}

func (r *AuthMetricsRecorder) RecordSecurityEvent(
	ctx context.Context,
	event authapp.SecurityMetricEvent,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordSecurityEvent(
		ctx,
		SecurityMetricEvent(event),
	)
}

func (r *AuthMetricsRecorder) RecordOTPDelivery(
	ctx context.Context,
	channel otpinfra.DeliveryMetricChannel,
	provider otpinfra.DeliveryMetricProvider,
	outcome otpinfra.DeliveryMetricOutcome,
	duration time.Duration,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordOTPDelivery(
		ctx,
		OTPMetricChannel(channel),
		OTPMetricProvider(provider),
		MetricOutcome(outcome),
		duration,
	)
}
func (r *AuthMetricsRecorder) RecordAccessTokenVerification(
	ctx context.Context,
	outcome tokeninfra.AccessTokenVerificationMetricOutcome,
	duration time.Duration,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordAuthOperation(
		ctx,
		AuthMetricOperationAccessTokenVerify,
		MetricOutcome(outcome),
		duration,
	)
}
func (r *AuthMetricsRecorder) RecordOTPProviderHealthEvent(
	ctx context.Context,
	channel otpinfra.DeliveryMetricChannel,
	provider otpinfra.DeliveryMetricProvider,
	event otpinfra.ProviderHealthMetricEvent,
) {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.RecordOTPProviderHealthEvent(
		ctx,
		OTPMetricChannel(channel),
		OTPMetricProvider(provider),
		event,
	)
}
