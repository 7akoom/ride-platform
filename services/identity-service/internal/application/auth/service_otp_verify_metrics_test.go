package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyOTPRecordsRejectedMetricForInvalidOTP(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()

	challengeRepository := &testChallengeRepository{
		findResult: verifyOTPMetricsChallenge(
			fixedTime,
		),
	}

	otpHasher := &testOTPHasher{
		compareMatchesSet: true,
		compareMatches:    false,
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		otpHasher,
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "000000",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if !errors.Is(
		err,
		ErrInvalidOTP,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrInvalidOTP,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeRejected,
	)

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogin,
		MetricOutcomeRejected,
	)

	requirePositiveAuthOperationDuration(
		t,
		metricsRecorder,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)
}

func TestVerifyOTPRecordsChallengeReplaySecurityMetric(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()
	verifiedAt := fixedTime.Add(
		-time.Minute,
	)

	challenge := verifyOTPMetricsChallenge(
		fixedTime,
	)
	challenge.VerifiedAt = &verifiedAt

	challengeRepository := &testChallengeRepository{
		findResult: challenge,
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "123456",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if !errors.Is(
		err,
		ErrChallengeUsed,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeUsed,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeRejected,
	)

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogin,
		MetricOutcomeRejected,
	)

	requirePositiveAuthOperationDuration(
		t,
		metricsRecorder,
	)

	requireSingleSecurityMetric(
		t,
		metricsRecorder,
		SecurityMetricEventChallengeReplay,
	)
}

func TestVerifyOTPRecordsAttemptsExceededSecurityMetric(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()

	challenge := verifyOTPMetricsChallenge(
		fixedTime,
	)
	challenge.FailedAttempts = challenge.MaxAttempts

	challengeRepository := &testChallengeRepository{
		findResult: challenge,
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "123456",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if !errors.Is(
		err,
		ErrChallengeAttemptsExceeded,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeAttemptsExceeded,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeRejected,
	)

	requireSingleSecurityMetric(
		t,
		metricsRecorder,
		SecurityMetricEventOTPAttemptsExceeded,
	)
}

func TestVerifyOTPRecordsFailedMetricWhenHasherFails(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()

	challengeRepository := &testChallengeRepository{
		findResult: verifyOTPMetricsChallenge(
			fixedTime,
		),
	}

	otpHasher := &testOTPHasher{
		compareErr: errors.New(
			"compare failed",
		),
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		otpHasher,
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "123456",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if err == nil {
		t.Fatal(
			"VerifyOTP() expected an error",
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeFailed,
	)

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogin,
		MetricOutcomeFailed,
	)

	requirePositiveAuthOperationDuration(
		t,
		metricsRecorder,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)
}

func verifyOTPMetricsTestTime() time.Time {
	return time.Date(
		2026,
		time.August,
		10,
		6,
		0,
		0,
		0,
		time.UTC,
	)
}

func verifyOTPMetricsChallenge(
	now time.Time,
) OTPChallenge {
	return OTPChallenge{
		ID: "otp_ch_metrics",
		Identifier: Identifier{
			Type:  IdentifierTypePhone,
			Value: "+9647501234567",
		},
		Purpose:        OTPPurposeLogin,
		CodeHash:       "hashed-code",
		ExpiresAt:      now.Add(5 * time.Minute),
		MaxAttempts:    5,
		FailedAttempts: 0,
	}
}

func TestVerifyOTPRecordsSuccessMetricAfterLoginCompletion(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()

	challengeRepository := &testChallengeRepository{
		findResult: verifyOTPMetricsChallenge(
			fixedTime,
		),
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_metrics",
			IsActive: true,
		},
		findFound: true,
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "123456",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeSuccess,
	)

	requireSingleAuthOperationMetric(
		t,
		metricsRecorder,
		AuthMetricOperationLogin,
		MetricOutcomeSuccess,
	)

	requirePositiveAuthOperationDuration(
		t,
		metricsRecorder,
	)

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationCreate,
		MetricOutcomeSuccess,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)
}

func TestVerifyOTPRecordsChallengeReplayWhenLoginCompletionDetectsUsedChallenge(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()

	challengeRepository := &testChallengeRepository{
		findResult: verifyOTPMetricsChallenge(
			fixedTime,
		),
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_metrics",
			IsActive: true,
		},
		findFound: true,
	}

	tokenIssuer := &testTokenIssuer{
		err: ErrChallengeUsed,
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		tokenIssuer,
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "123456",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if !errors.Is(
		err,
		ErrChallengeUsed,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeUsed,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeRejected,
	)

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationCreate,
		MetricOutcomeRejected,
	)

	requireSingleSecurityMetric(
		t,
		metricsRecorder,
		SecurityMetricEventChallengeReplay,
	)
}

func TestVerifyOTPRecordsSuccessMetricAfterIdentifierLinkCompletion(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()
	targetIdentityID := "identity_link_metrics"

	challenge := verifyOTPMetricsChallenge(
		fixedTime,
	)
	challenge.Purpose = OTPPurposeLinkIdentifier
	challenge.TargetIdentityID = &targetIdentityID

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		&testChallengeRepository{
			findResult: challenge,
		},
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:              "otp_ch_metrics",
			Code:                     "123456",
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &targetIdentityID,
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if result.IdentityID != targetIdentityID {
		t.Fatalf(
			"IdentityID = %q, expected %q",
			result.IdentityID,
			targetIdentityID,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLinkIdentifier,
		MetricOutcomeSuccess,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)

	requireNoAuthOperationMetrics(
		t,
		metricsRecorder,
	)
}

func TestVerifyOTPRecordsRejectedMetricWhenIdentifierAlreadyLinked(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()
	targetIdentityID := "identity_link_metrics"

	challenge := verifyOTPMetricsChallenge(
		fixedTime,
	)
	challenge.Purpose = OTPPurposeLinkIdentifier
	challenge.TargetIdentityID = &targetIdentityID

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		&testChallengeRepository{
			findResult: challenge,
		},
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{
			err: ErrIdentifierAlreadyLinked,
		},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:              "otp_ch_metrics",
			Code:                     "123456",
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &targetIdentityID,
		},
	)
	if !errors.Is(
		err,
		ErrIdentifierAlreadyLinked,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrIdentifierAlreadyLinked,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLinkIdentifier,
		MetricOutcomeRejected,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)

	requireNoAuthOperationMetrics(
		t,
		metricsRecorder,
	)
}

func TestVerifyOTPRecordsSuccessMetricAfterIdentifierUnlinkCompletion(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()
	targetIdentityID := "identity_unlink_metrics"

	challenge := verifyOTPMetricsChallenge(
		fixedTime,
	)
	challenge.Purpose = OTPPurposeUnlinkIdentifier
	challenge.TargetIdentityID = &targetIdentityID

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceWithUnlinkStoreForTest(
		&testChallengeRepository{
			findResult: challenge,
		},
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:              "otp_ch_metrics",
			Code:                     "123456",
			ExpectedPurpose:          OTPPurposeUnlinkIdentifier,
			ExpectedTargetIdentityID: &targetIdentityID,
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if result.IdentityID != targetIdentityID {
		t.Fatalf(
			"IdentityID = %q, expected %q",
			result.IdentityID,
			targetIdentityID,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeUnlinkIdentifier,
		MetricOutcomeSuccess,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)

	requireNoAuthOperationMetrics(
		t,
		metricsRecorder,
	)
}

func TestVerifyOTPRecordsRejectedMetricWhenLastIdentifierRemovalIsRejected(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()
	targetIdentityID := "identity_unlink_metrics"

	challenge := verifyOTPMetricsChallenge(
		fixedTime,
	)
	challenge.Purpose = OTPPurposeUnlinkIdentifier
	challenge.TargetIdentityID = &targetIdentityID

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceWithUnlinkStoreForTest(
		&testChallengeRepository{
			findResult: challenge,
		},
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testIdentifierUnlinkCompletionStore{
			err: ErrLastIdentifierRemoval,
		},
		&testOTPHasher{},
		&testTokenIssuer{},
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:              "otp_ch_metrics",
			Code:                     "123456",
			ExpectedPurpose:          OTPPurposeUnlinkIdentifier,
			ExpectedTargetIdentityID: &targetIdentityID,
		},
	)
	if !errors.Is(
		err,
		ErrLastIdentifierRemoval,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrLastIdentifierRemoval,
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeUnlinkIdentifier,
		MetricOutcomeRejected,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)

	requireNoAuthOperationMetrics(
		t,
		metricsRecorder,
	)
}

func requireSingleOTPVerificationMetric(
	t *testing.T,
	recorder *testMetricsRecorder,
	purpose OTPPurpose,
	outcome MetricOutcome,
) {
	t.Helper()

	if len(recorder.otpVerifications) != 1 {
		t.Fatalf(
			"OTP verification metric count = %d, expected 1",
			len(recorder.otpVerifications),
		)
	}

	metricValue := recorder.otpVerifications[0]

	if metricValue.purpose != purpose {
		t.Fatalf(
			"OTP verification purpose = %q, expected %q",
			metricValue.purpose,
			purpose,
		)
	}

	if metricValue.outcome != outcome {
		t.Fatalf(
			"OTP verification outcome = %q, expected %q",
			metricValue.outcome,
			outcome,
		)
	}
}

func requireSingleSecurityMetric(
	t *testing.T,
	recorder *testMetricsRecorder,
	expected SecurityMetricEvent,
) {
	t.Helper()

	if len(recorder.securityEvents) != 1 {
		t.Fatalf(
			"security metric count = %d, expected 1",
			len(recorder.securityEvents),
		)
	}

	if recorder.securityEvents[0] != expected {
		t.Fatalf(
			"security event = %q, expected %q",
			recorder.securityEvents[0],
			expected,
		)
	}
}

func requireNoSecurityMetrics(
	t *testing.T,
	recorder *testMetricsRecorder,
) {
	t.Helper()

	if len(recorder.securityEvents) != 0 {
		t.Fatalf(
			"security metric count = %d, expected 0",
			len(recorder.securityEvents),
		)
	}
}
func TestVerifyOTPRecordsFailedSessionCreateWhenTokenIssuerFails(
	t *testing.T,
) {
	fixedTime := verifyOTPMetricsTestTime()

	challengeRepository := &testChallengeRepository{
		findResult: verifyOTPMetricsChallenge(
			fixedTime,
		),
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_metrics",
			IsActive: true,
		},
		findFound: true,
	}

	tokenIssuer := &testTokenIssuer{
		err: errors.New(
			"token issuer failed",
		),
	}

	metricsRecorder := &testMetricsRecorder{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		tokenIssuer,
		&testClock{
			now: fixedTime,
		},
		WithMetricsRecorder(metricsRecorder),
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID:     "otp_ch_metrics",
			Code:            "123456",
			ExpectedPurpose: OTPPurposeLogin,
		},
	)
	if err == nil {
		t.Fatal(
			"VerifyOTP() expected an error",
		)
	}

	requireSingleOTPVerificationMetric(
		t,
		metricsRecorder,
		OTPPurposeLogin,
		MetricOutcomeFailed,
	)

	requireSingleSessionOperationMetric(
		t,
		metricsRecorder,
		SessionMetricOperationCreate,
		MetricOutcomeFailed,
	)

	requireNoSecurityMetrics(
		t,
		metricsRecorder,
	)
}
func requirePositiveAuthOperationDuration(
	t *testing.T,
	recorder *testMetricsRecorder,
) {
	t.Helper()

	if len(recorder.authOperations) != 1 {
		t.Fatalf(
			"auth operation metric count = %d, expected 1",
			len(recorder.authOperations),
		)
	}

	if recorder.authOperations[0].duration <= 0 {
		t.Fatalf(
			"auth operation duration = %v, expected positive duration",
			recorder.authOperations[0].duration,
		)
	}
}
func requireNoAuthOperationMetrics(
	t *testing.T,
	recorder *testMetricsRecorder,
) {
	t.Helper()

	if len(recorder.authOperations) != 0 {
		t.Fatalf(
			"auth operation metric count = %d, expected 0",
			len(recorder.authOperations),
		)
	}
}
