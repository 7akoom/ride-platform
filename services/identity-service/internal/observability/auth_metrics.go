package observability

import (
	"context"
	"fmt"
	"time"

	otpinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const authMetricsInstrumentationName = "identity-service/auth"

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

type OTPMetricPurpose string

const (
	OTPMetricPurposeLogin            OTPMetricPurpose = "login"
	OTPMetricPurposeIdentifierLink   OTPMetricPurpose = "link_identifier"
	OTPMetricPurposeIdentifierUnlink OTPMetricPurpose = "unlink_identifier"
)

type OTPMetricChannel string

const (
	OTPMetricChannelAuto     OTPMetricChannel = "auto"
	OTPMetricChannelSMS      OTPMetricChannel = "sms"
	OTPMetricChannelWhatsApp OTPMetricChannel = "whatsapp"
	OTPMetricChannelEmail    OTPMetricChannel = "email"
)

type OTPMetricProvider string

const (
	OTPMetricProviderTelnyx       OTPMetricProvider = "telnyx"
	OTPMetricProviderMeta         OTPMetricProvider = "meta"
	OTPMetricProviderBulkSMSIraq  OTPMetricProvider = "bulksmsiraq"
	OTPMetricProviderResend       OTPMetricProvider = "resend"
	OTPMetricProviderDevelopment  OTPMetricProvider = "development"
	OTPMetricProviderUnconfigured OTPMetricProvider = "unconfigured"
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

type AuthMetrics struct {
	authOperations          metric.Int64Counter
	authDuration            metric.Float64Histogram
	otpRequests             metric.Int64Counter
	otpVerifications        metric.Int64Counter
	otpDeliveries           metric.Int64Counter
	otpDeliveryDuration     metric.Float64Histogram
	otpProviderHealthEvents metric.Int64Counter
	sessionOperations       metric.Int64Counter
	securityEvents          metric.Int64Counter
	deliveryWebhooks        metric.Int64Counter
}

func NewAuthMetrics() (*AuthMetrics, error) {
	return NewAuthMetricsWithMeter(
		otel.Meter(authMetricsInstrumentationName),
	)
}

func NewAuthMetricsWithMeter(
	meter metric.Meter,
) (*AuthMetrics, error) {
	if meter == nil {
		return nil, fmt.Errorf("metrics meter is required")
	}

	authOperations, err := meter.Int64Counter(
		"identity.auth.operations",
		metric.WithDescription(
			"Number of authentication operations.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create authentication operations counter: %w",
			err,
		)
	}

	authDuration, err := meter.Float64Histogram(
		"identity.auth.duration",
		metric.WithDescription(
			"Authentication operation duration.",
		),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create authentication duration histogram: %w",
			err,
		)
	}

	otpRequests, err := meter.Int64Counter(
		"identity.otp.requests",
		metric.WithDescription(
			"Number of OTP request attempts.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create OTP requests counter: %w",
			err,
		)
	}

	otpVerifications, err := meter.Int64Counter(
		"identity.otp.verifications",
		metric.WithDescription(
			"Number of OTP verification attempts.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create OTP verifications counter: %w",
			err,
		)
	}

	otpDeliveries, err := meter.Int64Counter(
		"identity.otp.deliveries",
		metric.WithDescription(
			"Number of OTP delivery attempts.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create OTP deliveries counter: %w",
			err,
		)
	}

	otpDeliveryDuration, err := meter.Float64Histogram(
		"identity.otp.delivery.duration",
		metric.WithDescription(
			"OTP provider delivery duration.",
		),
		metric.WithUnit("s"),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"create OTP delivery duration histogram: %w",
			err,
		)
	}

	otpProviderHealthEvents, err := meter.Int64Counter(
		"identity.otp.provider_health.events",
		metric.WithDescription(
			"Number of OTP provider health events.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create OTP provider health events counter: %w",
			err,
		)
	}

	sessionOperations, err := meter.Int64Counter(
		"identity.sessions.operations",
		metric.WithDescription(
			"Number of session lifecycle operations.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create session operations counter: %w",
			err,
		)
	}

	securityEvents, err := meter.Int64Counter(
		"identity.security.events",
		metric.WithDescription(
			"Number of authentication security events.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create security events counter: %w",
			err,
		)
	}

	deliveryWebhooks, err := meter.Int64Counter("identity.otp.delivery.webhooks",
		metric.WithDescription("Number of delivery webhook POST requests by processing outcome."))
	if err != nil {
		return nil, fmt.Errorf("create delivery webhook counter: %w", err)
	}

	return &AuthMetrics{
		authOperations:          authOperations,
		authDuration:            authDuration,
		otpRequests:             otpRequests,
		otpVerifications:        otpVerifications,
		otpDeliveries:           otpDeliveries,
		otpDeliveryDuration:     otpDeliveryDuration,
		otpProviderHealthEvents: otpProviderHealthEvents,
		sessionOperations:       sessionOperations,
		securityEvents:          securityEvents,
		deliveryWebhooks:        deliveryWebhooks,
	}, nil
}

func (m *AuthMetrics) RecordAuthOperation(
	ctx context.Context,
	operation AuthMetricOperation,
	outcome MetricOutcome,
	duration time.Duration,
) {
	if m == nil {
		return
	}

	attributes := metric.WithAttributes(
		attribute.String(
			"operation",
			string(operation),
		),
		attribute.String(
			"outcome",
			string(outcome),
		),
	)

	m.authOperations.Add(
		ctx,
		1,
		attributes,
	)

	m.authDuration.Record(
		ctx,
		duration.Seconds(),
		attributes,
	)
}

func (m *AuthMetrics) RecordOTPRequest(
	ctx context.Context,
	purpose OTPMetricPurpose,
	channel OTPMetricChannel,
	outcome MetricOutcome,
) {
	if m == nil {
		return
	}

	m.otpRequests.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String(
				"purpose",
				string(purpose),
			),
			attribute.String(
				"channel",
				string(channel),
			),
			attribute.String(
				"outcome",
				string(outcome),
			),
		),
	)
}

func (m *AuthMetrics) RecordOTPVerification(
	ctx context.Context,
	purpose OTPMetricPurpose,
	outcome MetricOutcome,
) {
	if m == nil {
		return
	}

	m.otpVerifications.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String(
				"purpose",
				string(purpose),
			),
			attribute.String(
				"outcome",
				string(outcome),
			),
		),
	)
}

func (m *AuthMetrics) RecordOTPDelivery(
	ctx context.Context,
	channel OTPMetricChannel,
	provider OTPMetricProvider,
	outcome MetricOutcome,
	duration time.Duration,
) {
	if m == nil {
		return
	}

	attributes := metric.WithAttributes(
		attribute.String(
			"channel",
			string(channel),
		),
		attribute.String(
			"provider",
			string(provider),
		),
		attribute.String(
			"outcome",
			string(outcome),
		),
	)

	m.otpDeliveries.Add(
		ctx,
		1,
		attributes,
	)

	m.otpDeliveryDuration.Record(
		ctx,
		duration.Seconds(),
		attributes,
	)
}

func (m *AuthMetrics) RecordSessionOperation(
	ctx context.Context,
	operation SessionMetricOperation,
	outcome MetricOutcome,
) {
	if m == nil {
		return
	}

	m.sessionOperations.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String(
				"operation",
				string(operation),
			),
			attribute.String(
				"outcome",
				string(outcome),
			),
		),
	)
}

func (m *AuthMetrics) RecordSecurityEvent(
	ctx context.Context,
	event SecurityMetricEvent,
) {
	if m == nil {
		return
	}

	m.securityEvents.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String(
				"event",
				string(event),
			),
		),
	)
}
func (m *AuthMetrics) RecordOTPProviderHealthEvent(
	ctx context.Context,
	channel OTPMetricChannel,
	provider OTPMetricProvider,
	event otpinfra.ProviderHealthMetricEvent,
) {
	if m == nil {
		return
	}

	m.otpProviderHealthEvents.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String(
				"channel",
				string(channel),
			),
			attribute.String(
				"provider",
				string(provider),
			),
			attribute.String(
				"event",
				string(event),
			),
		),
	)
}
