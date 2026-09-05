package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestOTPStopsBeforeGeneratingOTPWhenRateLimited(
	t *testing.T,
) {
	challengeRepository := &testChallengeRepository{}
	otpGenerator := &testOTPGenerator{}
	otpHasher := &testOTPHasher{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{
		err: ErrOTPRequestRateLimited,
	}
	challengeIDGenerator := &testChallengeIDGenerator{}
	metricsRecorder := &testMetricsRecorder{}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		otpGenerator,
		otpHasher,
		otpDelivery,
		rateLimiter,
		challengeIDGenerator,
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
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
				6,
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
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
		},
	)

	if !errors.Is(
		err,
		ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"RequestOTP() returned %v, expected %v",
			err,
			ErrOTPRequestRateLimited,
		)
	}

	if !rateLimiter.called {
		t.Fatal("OTP request rate limiter was not called")
	}

	if otpGenerator.called {
		t.Fatal(
			"OTP generator was called after request was rate limited",
		)
	}

	if otpHasher.hashCalled {
		t.Fatal(
			"OTP hasher was called after request was rate limited",
		)
	}

	if challengeIDGenerator.called {
		t.Fatal(
			"challenge ID generator was called after request was rate limited",
		)
	}

	if challengeRepository.createCalled {
		t.Fatal(
			"OTP challenge was created after request was rate limited",
		)
	}

	if otpDelivery.called {
		t.Fatal(
			"OTP delivery was called after request was rate limited",
		)
	}

	if len(metricsRecorder.otpRequests) != 1 {
		t.Fatalf(
			"OTP request metric count = %d, expected 1",
			len(metricsRecorder.otpRequests),
		)
	}

	requestMetric := metricsRecorder.otpRequests[0]

	if requestMetric.purpose != OTPPurposeLogin {
		t.Fatalf(
			"OTP request metric purpose = %q, expected %q",
			requestMetric.purpose,
			OTPPurposeLogin,
		)
	}

	if requestMetric.channel != OTPDeliveryChannelAuto {
		t.Fatalf(
			"OTP request metric channel = %q, expected %q",
			requestMetric.channel,
			OTPDeliveryChannelAuto,
		)
	}

	if requestMetric.outcome != MetricOutcomeRejected {
		t.Fatalf(
			"OTP request metric outcome = %q, expected %q",
			requestMetric.outcome,
			MetricOutcomeRejected,
		)
	}

	if len(metricsRecorder.securityEvents) != 1 {
		t.Fatalf(
			"security event metric count = %d, expected 1",
			len(metricsRecorder.securityEvents),
		)
	}

	if metricsRecorder.securityEvents[0] !=
		SecurityMetricEventOTPRateLimited {
		t.Fatalf(
			"security event = %q, expected %q",
			metricsRecorder.securityEvents[0],
			SecurityMetricEventOTPRateLimited,
		)
	}
}

func TestRequestOTPContinuesWhenRateLimiterAllows(
	t *testing.T,
) {
	challengeRepository := &testChallengeRepository{}
	otpGenerator := &testOTPGenerator{}
	otpHasher := &testOTPHasher{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}
	challengeIDGenerator := &testChallengeIDGenerator{}
	metricsRecorder := &testMetricsRecorder{}

	fixedTime := time.Date(
		2026,
		time.August,
		10,
		6,
		0,
		0,
		0,
		time.UTC,
	)

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		otpGenerator,
		otpHasher,
		otpDelivery,
		rateLimiter,
		challengeIDGenerator,
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: fixedTime,
		},
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
		WithMetricsRecorder(metricsRecorder),
	)

	result, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "  +9647501234567  ",
			},
			Purpose: OTPPurposeLogin,
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	if !rateLimiter.called {
		t.Fatal(
			"OTP request rate limiter was not called",
		)
	}

	if !otpGenerator.called {
		t.Fatal(
			"OTP generator was not called",
		)
	}

	if !otpHasher.hashCalled {
		t.Fatal(
			"OTP hasher was not called",
		)
	}

	if otpHasher.hashChallengeID != "otp_ch_test" {
		t.Fatalf(
			"OTP hasher challenge ID = %q, expected %q",
			otpHasher.hashChallengeID,
			"otp_ch_test",
		)
	}

	if otpHasher.hashCode != "123456" {
		t.Fatalf(
			"OTP hasher code = %q, expected %q",
			otpHasher.hashCode,
			"123456",
		)
	}

	if !challengeIDGenerator.called {
		t.Fatal(
			"challenge ID generator was not called",
		)
	}

	if !challengeRepository.createCalled {
		t.Fatal(
			"OTP challenge was not created",
		)
	}

	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not called",
		)
	}

	if result.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"ChallengeID is %q, expected %q",
			result.ChallengeID,
			"otp_ch_test",
		)
	}

	if result.ExpiresInSeconds != 300 {
		t.Fatalf(
			"ExpiresInSeconds is %d, expected 300",
			result.ExpiresInSeconds,
		)
	}

	if len(metricsRecorder.otpRequests) != 1 {
		t.Fatalf(
			"OTP request metric count = %d, expected 1",
			len(metricsRecorder.otpRequests),
		)
	}

	requestMetric := metricsRecorder.otpRequests[0]

	if requestMetric.purpose != OTPPurposeLogin {
		t.Fatalf(
			"OTP request metric purpose = %q, expected %q",
			requestMetric.purpose,
			OTPPurposeLogin,
		)
	}

	if requestMetric.channel != OTPDeliveryChannelAuto {
		t.Fatalf(
			"OTP request metric channel = %q, expected %q",
			requestMetric.channel,
			OTPDeliveryChannelAuto,
		)
	}

	if requestMetric.outcome != MetricOutcomeSuccess {
		t.Fatalf(
			"OTP request metric outcome = %q, expected %q",
			requestMetric.outcome,
			MetricOutcomeSuccess,
		)
	}

	if len(metricsRecorder.securityEvents) != 0 {
		t.Fatalf(
			"security event metric count = %d, expected 0",
			len(metricsRecorder.securityEvents),
		)
	}
}

type testOTPRequestMetric struct {
	purpose OTPPurpose
	channel OTPDeliveryChannel
	outcome MetricOutcome
}

type testOTPVerificationMetric struct {
	purpose OTPPurpose
	outcome MetricOutcome
}

type testAuthOperationMetric struct {
	operation AuthMetricOperation
	outcome   MetricOutcome
	duration  time.Duration
}

type testSessionOperationMetric struct {
	operation SessionMetricOperation
	outcome   MetricOutcome
}

type testMetricsRecorder struct {
	authOperations    []testAuthOperationMetric
	otpRequests       []testOTPRequestMetric
	otpVerifications  []testOTPVerificationMetric
	sessionOperations []testSessionOperationMetric
	securityEvents    []SecurityMetricEvent
}

func (r *testMetricsRecorder) RecordAuthOperation(
	_ context.Context,
	operation AuthMetricOperation,
	outcome MetricOutcome,
	duration time.Duration,
) {
	r.authOperations = append(
		r.authOperations,
		testAuthOperationMetric{
			operation: operation,
			outcome:   outcome,
			duration:  duration,
		},
	)
}

func (r *testMetricsRecorder) RecordOTPRequest(
	_ context.Context,
	purpose OTPPurpose,
	channel OTPDeliveryChannel,
	outcome MetricOutcome,
) {
	r.otpRequests = append(
		r.otpRequests,
		testOTPRequestMetric{
			purpose: purpose,
			channel: channel,
			outcome: outcome,
		},
	)
}

func (r *testMetricsRecorder) RecordOTPVerification(
	_ context.Context,
	purpose OTPPurpose,
	outcome MetricOutcome,
) {
	r.otpVerifications = append(
		r.otpVerifications,
		testOTPVerificationMetric{
			purpose: purpose,
			outcome: outcome,
		},
	)
}

func (r *testMetricsRecorder) RecordSessionOperation(
	_ context.Context,
	operation SessionMetricOperation,
	outcome MetricOutcome,
) {
	r.sessionOperations = append(
		r.sessionOperations,
		testSessionOperationMetric{
			operation: operation,
			outcome:   outcome,
		},
	)
}

func (r *testMetricsRecorder) RecordSecurityEvent(
	_ context.Context,
	event SecurityMetricEvent,
) {
	r.securityEvents = append(
		r.securityEvents,
		event,
	)
}
