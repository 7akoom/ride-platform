package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testChallengeRepository struct {
	createCalled              bool
	cancelCalled              bool
	recordFailedAttemptCalled bool
	markVerifiedCalled        bool

	findResult             OTPChallenge
	findErr                error
	recordFailedAttemptErr error
	markVerifiedErr        error

	cancelledChallengeID     string
	cancelledAt              time.Time
	cancelErr                error
	cancelContextErr         error
	cancelContextHasDeadline bool
	cancelContextDeadline    time.Time
}

func (r *testChallengeRepository) Create(
	ctx context.Context,
	challenge OTPChallenge,
) error {
	r.createCalled = true
	return nil
}

func (r *testChallengeRepository) FindByID(
	ctx context.Context,
	challengeID string,
) (OTPChallenge, error) {
	return r.findResult, r.findErr
}

func (r *testChallengeRepository) RecordFailedAttempt(
	ctx context.Context,
	challengeID string,
	attemptedAt time.Time,
) error {
	r.recordFailedAttemptCalled = true

	return r.recordFailedAttemptErr
}

func (r *testChallengeRepository) MarkVerified(
	ctx context.Context,
	challengeID string,
	verifiedAt time.Time,
) error {
	r.markVerifiedCalled = true

	return r.markVerifiedErr
}

func (r *testChallengeRepository) Cancel(
	ctx context.Context,
	challengeID string,
	cancelledAt time.Time,
) error {
	r.cancelCalled = true
	r.cancelledChallengeID = challengeID
	r.cancelledAt = cancelledAt
	r.cancelContextErr = ctx.Err()
	r.cancelContextDeadline, r.cancelContextHasDeadline =
		ctx.Deadline()

	return r.cancelErr
}

type testIdentityRepository struct {
	result Identity
	err    error
}

func (r *testIdentityRepository) FindOrCreateByPhoneNumber(
	ctx context.Context,
	phoneNumber string,
) (Identity, error) {
	if r.err != nil {
		return Identity{}, r.err
	}

	return r.result, nil
}

type testOTPGenerator struct {
	called bool
}

func (g *testOTPGenerator) Generate() (string, error) {
	g.called = true
	return "123456", nil
}

type testOTPHasher struct {
	hashCalled         bool
	hashChallengeID    string
	hashCode           string
	compareCalled      bool
	compareHash        string
	compareChallengeID string
	compareCode        string
	compareMatches     bool
	compareMatchesSet  bool
	compareErr         error
}

func (h *testOTPHasher) Hash(
	challengeID string,
	code string,
) (string, error) {
	h.hashCalled = true
	h.hashChallengeID = challengeID
	h.hashCode = code

	return "hashed-code", nil
}

func (h *testOTPHasher) Compare(
	hash string,
	challengeID string,
	code string,
) (bool, error) {
	h.compareCalled = true
	h.compareHash = hash
	h.compareChallengeID = challengeID
	h.compareCode = code

	if h.compareErr != nil {
		return false, h.compareErr
	}

	if h.compareMatchesSet {
		return h.compareMatches, nil
	}

	return true, nil
}

type testOTPDelivery struct {
	called bool
	err    error
	onSend func()
}

func (d *testOTPDelivery) Send(
	ctx context.Context,
	phoneNumber string,
	code string,
) error {
	d.called = true

	if d.onSend != nil {
		d.onSend()
	}

	return d.err
}

type testOTPRequestRateLimiter struct {
	called bool
	err    error
}

func (r *testOTPRequestRateLimiter) Allow(
	ctx context.Context,
	phoneNumber string,
	now time.Time,
	policy OTPRequestRateLimitPolicy,
) error {
	r.called = true
	return r.err
}

type testChallengeIDGenerator struct {
	called bool
}

func (g *testChallengeIDGenerator) Generate() (string, error) {
	g.called = true
	return "otp_ch_test", nil
}

type testTokenIssuer struct {
	result TokenPair
	err    error
	calls  int
	input  TokenIssueInput
}

func (i *testTokenIssuer) Issue(
	ctx context.Context,
	input TokenIssueInput,
) (TokenPair, error) {
	i.calls++
	i.input = input

	return i.result, i.err
}

type testRefreshTokenRotationStore struct {
	inspectResult RefreshTokenContext
	inspectErr    error
	rotateErr     error

	inspectCalls int
	rotateCalls  int

	inspectedTokenHash string
	inspectedAt        time.Time

	rotationInput RefreshTokenRotationInput
}

func (s *testRefreshTokenRotationStore) Inspect(
	ctx context.Context,
	currentTokenHash string,
	now time.Time,
) (RefreshTokenContext, error) {
	s.inspectCalls++
	s.inspectedTokenHash = currentTokenHash
	s.inspectedAt = now

	return s.inspectResult, s.inspectErr
}

func (s *testRefreshTokenRotationStore) Rotate(
	ctx context.Context,
	input RefreshTokenRotationInput,
) error {
	s.rotateCalls++
	s.rotationInput = input

	return s.rotateErr
}

type testSessionRevocationStore struct {
	err   error
	calls int

	refreshTokenHash string
	revokedAt        time.Time
}

func (s *testSessionRevocationStore) RevokeByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	s.calls++
	s.refreshTokenHash = refreshTokenHash
	s.revokedAt = revokedAt

	return s.err
}

type testAllSessionsRevocationStore struct {
	err   error
	calls int

	refreshTokenHash string
	revokedAt        time.Time
}

func (s *testAllSessionsRevocationStore) RevokeAllByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	s.calls++
	s.refreshTokenHash = refreshTokenHash
	s.revokedAt = revokedAt

	return s.err
}

type testRefreshTokenGenerator struct {
	token string
	err   error
	calls int
}

func (g *testRefreshTokenGenerator) Generate() (string, error) {
	g.calls++

	if g.err != nil {
		return "", g.err
	}

	if g.token != "" {
		return g.token, nil
	}

	return "rt_test_replacement", nil
}

type testRefreshTokenHasher struct {
	calls  int
	inputs []string
}

func (h *testRefreshTokenHasher) Hash(
	refreshToken string,
) string {
	h.calls++
	h.inputs = append(
		h.inputs,
		refreshToken,
	)

	return "hashed_" + refreshToken
}

type testAccessTokenSigner struct {
	accessToken      string
	expiresInSeconds int32
	err              error
	calls            int

	identityID       string
	sessionID        string
	issuedAt         time.Time
	sessionExpiresAt time.Time
}

func (s *testAccessTokenSigner) Issue(
	identityID string,
	sessionID string,
	issuedAt time.Time,
) (string, int32, error) {
	s.calls++
	s.identityID = identityID
	s.sessionID = sessionID
	s.issuedAt = issuedAt

	if s.err != nil {
		return "", 0, s.err
	}

	accessToken := s.accessToken
	if accessToken == "" {
		accessToken = "test-access-token"
	}

	expiresInSeconds := s.expiresInSeconds
	if expiresInSeconds == 0 {
		expiresInSeconds = 900
	}

	return accessToken, expiresInSeconds, nil
}

func (s *testAccessTokenSigner) IssueForSession(
	identityID string,
	sessionID string,
	issuedAt time.Time,
	sessionExpiresAt time.Time,
) (string, int32, error) {
	s.calls++
	s.identityID = identityID
	s.sessionID = sessionID
	s.issuedAt = issuedAt
	s.sessionExpiresAt = sessionExpiresAt

	if s.err != nil {
		return "", 0, s.err
	}

	accessToken := s.accessToken
	if accessToken == "" {
		accessToken = "test-access-token"
	}

	expiresInSeconds := s.expiresInSeconds
	if expiresInSeconds == 0 {
		expiresInSeconds = 900
	}

	return accessToken, expiresInSeconds, nil
}

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
}

type serviceConstructorTestDependencies struct {
	challengeRepository        ChallengeRepository
	identityRepository         IdentityRepository
	otpGenerator               OTPGenerator
	otpHasher                  OTPHasher
	otpDelivery                OTPDelivery
	otpRequestRateLimiter      OTPRequestRateLimiter
	challengeIDGenerator       ChallengeIDGenerator
	tokenIssuer                TokenIssuer
	refreshTokenRotationStore  RefreshTokenRotationStore
	sessionRevocationStore     SessionRevocationStore
	allSessionsRevocationStore AllSessionsRevocationStore
	refreshTokenGenerator      RefreshTokenGenerator
	refreshTokenHasher         RefreshTokenHasher
	accessTokenSigner          AccessTokenSigner
	clock                      Clock

	otpTTL                    time.Duration
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy
	refreshTokenTTL           time.Duration
}

func newValidServiceConstructorTestDependencies() serviceConstructorTestDependencies {
	return serviceConstructorTestDependencies{
		challengeRepository:        &testChallengeRepository{},
		identityRepository:         &testIdentityRepository{},
		otpGenerator:               &testOTPGenerator{},
		otpHasher:                  &testOTPHasher{},
		otpDelivery:                &testOTPDelivery{},
		otpRequestRateLimiter:      &testOTPRequestRateLimiter{},
		challengeIDGenerator:       &testChallengeIDGenerator{},
		tokenIssuer:                &testTokenIssuer{},
		refreshTokenRotationStore:  &testRefreshTokenRotationStore{},
		sessionRevocationStore:     &testSessionRevocationStore{},
		allSessionsRevocationStore: &testAllSessionsRevocationStore{},
		refreshTokenGenerator:      &testRefreshTokenGenerator{},
		refreshTokenHasher:         &testRefreshTokenHasher{},
		accessTokenSigner:          &testAccessTokenSigner{},
		clock:                      &testClock{},

		otpTTL: 5 * time.Minute,
		otpRequestRateLimitPolicy: OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		refreshTokenTTL: 29 * 24 * time.Hour,
	}
}

func newServiceFromConstructorTestDependencies(
	dependencies serviceConstructorTestDependencies,
) Service {
	return NewService(
		dependencies.challengeRepository,
		dependencies.identityRepository,
		dependencies.otpGenerator,
		dependencies.otpHasher,
		dependencies.otpDelivery,
		dependencies.otpRequestRateLimiter,
		dependencies.challengeIDGenerator,
		dependencies.tokenIssuer,
		dependencies.refreshTokenRotationStore,
		dependencies.sessionRevocationStore,
		dependencies.allSessionsRevocationStore,
		dependencies.refreshTokenGenerator,
		dependencies.refreshTokenHasher,
		dependencies.accessTokenSigner,
		dependencies.clock,
		dependencies.otpTTL,
		dependencies.otpRequestRateLimitPolicy,
		dependencies.refreshTokenTTL,
	)
}

func TestNewServicePanicsForInvalidConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*serviceConstructorTestDependencies)
	}{
		{
			name: "nil challenge repository",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.challengeRepository = nil
			},
		},
		{
			name: "nil identity repository",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identityRepository = nil
			},
		},
		{
			name: "nil OTP generator",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpGenerator = nil
			},
		},
		{
			name: "nil OTP hasher",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpHasher = nil
			},
		},
		{
			name: "nil OTP delivery",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpDelivery = nil
			},
		},
		{
			name: "nil OTP request rate limiter",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimiter = nil
			},
		},
		{
			name: "nil challenge ID generator",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.challengeIDGenerator = nil
			},
		},
		{
			name: "nil token issuer",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.tokenIssuer = nil
			},
		},
		{
			name: "nil refresh token rotation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenRotationStore = nil
			},
		},
		{
			name: "nil session revocation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.sessionRevocationStore = nil
			},
		},
		{
			name: "nil all sessions revocation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.allSessionsRevocationStore = nil
			},
		},
		{
			name: "nil refresh token generator",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenGenerator = nil
			},
		},
		{
			name: "nil refresh token hasher",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenHasher = nil
			},
		},
		{
			name: "nil access token signer",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.accessTokenSigner = nil
			},
		},
		{
			name: "nil clock",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.clock = nil
			},
		},
		{
			name: "zero OTP TTL",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpTTL = 0
			},
		},
		{
			name: "zero OTP request cooldown",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.Cooldown = 0
			},
		},
		{
			name: "zero OTP request window",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.Window = 0
			},
		},
		{
			name: "zero OTP request max requests",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.MaxRequests = 0
			},
		},
		{
			name: "OTP cooldown exceeds window",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.Cooldown =
					d.otpRequestRateLimitPolicy.Window + time.Second
			},
		},
		{
			name: "zero refresh token TTL",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenTTL = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependencies :=
				newValidServiceConstructorTestDependencies()

			tt.mutate(&dependencies)

			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal(
						"NewService() did not panic for invalid configuration",
					)
				}
			}()

			newServiceFromConstructorTestDependencies(
				dependencies,
			)
		})
	}
}

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

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		otpGenerator,
		otpHasher,
		otpDelivery,
		rateLimiter,
		challengeIDGenerator,
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
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
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			PhoneNumber: "+9647501234567",
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

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		otpGenerator,
		otpHasher,
		otpDelivery,
		rateLimiter,
		challengeIDGenerator,
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
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
	)

	result, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			PhoneNumber: "  +9647501234567  ",
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
}

func TestRequestOTPCancelsChallengeWhenDeliveryFails(
	t *testing.T,
) {
	deliveryError := errors.New("SMS provider unavailable")

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{
		err: deliveryError,
	}

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		otpDelivery,
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
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
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			PhoneNumber: "+9647501234567",
		},
	)

	if err == nil {
		t.Fatal(
			"RequestOTP() returned nil error when delivery failed",
		)
	}

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"RequestOTP() returned %v, expected delivery error",
			err,
		)
	}

	if !challengeRepository.createCalled {
		t.Fatal(
			"OTP challenge was not created before delivery",
		)
	}

	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not attempted",
		)
	}

	if !challengeRepository.cancelCalled {
		t.Fatal(
			"OTP challenge was not cancelled after delivery failure",
		)
	}

	if challengeRepository.cancelledChallengeID != "otp_ch_test" {
		t.Fatalf(
			"cancelled challenge ID is %q, expected %q",
			challengeRepository.cancelledChallengeID,
			"otp_ch_test",
		)
	}
}

func TestRequestOTPCancelsChallengeWithIndependentBoundedContextWhenRequestIsCancelled(
	t *testing.T,
) {
	deliveryError := errors.New(
		"SMS delivery interrupted",
	)

	requestCtx, cancelRequest :=
		context.WithCancel(context.Background())
	defer cancelRequest()

	challengeRepository := &testChallengeRepository{}

	otpDelivery := &testOTPDelivery{
		err: deliveryError,
		onSend: func() {
			cancelRequest()
		},
	}

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		otpDelivery,
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: time.Date(
				2026,
				time.August,
				12,
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
	)

	_, err := service.RequestOTP(
		requestCtx,
		RequestOTPInput{
			PhoneNumber: "+9647501234567",
		},
	)

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"RequestOTP() error = %v, expected delivery error",
			err,
		)
	}

	if requestCtx.Err() != context.Canceled {
		t.Fatalf(
			"request context error = %v, expected context canceled",
			requestCtx.Err(),
		)
	}

	if !challengeRepository.cancelCalled {
		t.Fatal(
			"OTP challenge was not cancelled after delivery failure",
		)
	}

	if challengeRepository.cancelContextErr != nil {
		t.Fatalf(
			"Cancel() received cancelled context: %v",
			challengeRepository.cancelContextErr,
		)
	}

	if !challengeRepository.cancelContextHasDeadline {
		t.Fatal(
			"Cancel() compensation context has no deadline",
		)
	}
}

func TestRequestOTPReturnsDeliveryAndCancellationErrors(
	t *testing.T,
) {
	deliveryError := errors.New(
		"SMS provider unavailable",
	)

	cancellationError := errors.New(
		"challenge cancellation failed",
	)

	challengeRepository := &testChallengeRepository{
		cancelErr: cancellationError,
	}

	otpDelivery := &testOTPDelivery{
		err: deliveryError,
	}

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		otpDelivery,
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		&testClock{
			now: time.Date(
				2026,
				time.August,
				12,
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
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			PhoneNumber: "+9647501234567",
		},
	)

	if err == nil {
		t.Fatal(
			"RequestOTP() returned nil error when delivery and cancellation failed",
		)
	}

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"RequestOTP() error = %v, expected delivery error to be preserved",
			err,
		)
	}

	if !errors.Is(err, cancellationError) {
		t.Fatalf(
			"RequestOTP() error = %v, expected cancellation error to be preserved",
			err,
		)
	}

	if !challengeRepository.cancelCalled {
		t.Fatal(
			"OTP challenge cancellation was not attempted after delivery failure",
		)
	}
}

func TestVerifyOTPMapsConcurrentCancellationFromRecordFailedAttempt(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:             "otp_ch_test",
			PhoneNumber:    "+9647501234567",
			CodeHash:       "hashed-code",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
		recordFailedAttemptErr: ErrChallengeCancelled,
	}

	otpHasher := &testOTPHasher{
		compareMatchesSet: true,
		compareMatches:    false,
	}

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		&testOTPGenerator{},
		otpHasher,
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
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

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID: "otp_ch_test",
			Code:        "000000",
		},
	)

	if !errors.Is(
		err,
		ErrChallengeCancelled,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeCancelled,
		)
	}

	if !otpHasher.compareCalled {
		t.Fatal(
			"OTP hasher Compare() was not called",
		)
	}

	if otpHasher.compareHash != "hashed-code" {
		t.Fatalf(
			"OTP hasher comparison hash = %q, expected %q",
			otpHasher.compareHash,
			"hashed-code",
		)
	}

	if otpHasher.compareChallengeID != "otp_ch_test" {
		t.Fatalf(
			"OTP hasher comparison challenge ID = %q, expected %q",
			otpHasher.compareChallengeID,
			"otp_ch_test",
		)
	}

	if otpHasher.compareCode != "000000" {
		t.Fatalf(
			"OTP hasher comparison code = %q, expected %q",
			otpHasher.compareCode,
			"000000",
		)
	}

	if !challengeRepository.recordFailedAttemptCalled {
		t.Fatal(
			"RecordFailedAttempt() was not called",
		)
	}

	if challengeRepository.markVerifiedCalled {
		t.Fatal(
			"MarkVerified() was called after concurrent cancellation",
		)
	}
}

func TestVerifyOTPDoesNotRecordFailedAttemptWhenHasherFails(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		12,
		7,
		0,
		0,
		0,
		time.UTC,
	)

	compareError := errors.New(
		"corrupted OTP hash",
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:             "otp_ch_test",
			PhoneNumber:    "+9647501234567",
			CodeHash:       "corrupted-hash",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	otpHasher := &testOTPHasher{
		compareErr: compareError,
	}

	service := NewService(
		challengeRepository,
		&testIdentityRepository{},
		&testOTPGenerator{},
		otpHasher,
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
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

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID: "otp_ch_test",
			Code:        "123456",
		},
	)

	if err == nil {
		t.Fatal(
			"VerifyOTP() returned nil error when OTP hasher failed",
		)
	}

	if !errors.Is(err, compareError) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected wrapped hasher error",
			err,
		)
	}

	if challengeRepository.recordFailedAttemptCalled {
		t.Fatal(
			"RecordFailedAttempt() was called when OTP hasher failed",
		)
	}
}

func TestVerifyOTPMapsConcurrentCancellationFromTokenIssuer(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:             "otp_ch_test",
			PhoneNumber:    "+9647501234567",
			CodeHash:       "hashed-code",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identity := Identity{
		ID:          "identity-123",
		PhoneNumber: "+9647501234567",
		IsActive:    true,
	}

	identityRepository := &testIdentityRepository{
		result: identity,
	}

	tokenIssuer := &testTokenIssuer{
		err: ErrChallengeCancelled,
	}

	service := NewService(
		challengeRepository,
		identityRepository,
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		tokenIssuer,
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
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

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ChallengeID: "otp_ch_test",
			Code:        "123456",
		},
	)

	if !errors.Is(
		err,
		ErrChallengeCancelled,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrChallengeCancelled,
		)
	}

	if tokenIssuer.calls != 1 {
		t.Fatalf(
			"TokenIssuer.Issue() calls = %d, expected 1",
			tokenIssuer.calls,
		)
	}

	if tokenIssuer.input.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"TokenIssuer.Issue() challenge ID = %q, expected %q",
			tokenIssuer.input.ChallengeID,
			"otp_ch_test",
		)
	}

	if !tokenIssuer.input.VerifiedAt.Equal(now) {
		t.Fatalf(
			"TokenIssuer.Issue() verification time = %v, expected %v",
			tokenIssuer.input.VerifiedAt,
			now,
		)
	}

	if tokenIssuer.input.Identity.ID != identity.ID {
		t.Fatalf(
			"TokenIssuer.Issue() identity ID = %q, expected %q",
			tokenIssuer.input.Identity.ID,
			identity.ID,
		)
	}

	if challengeRepository.markVerifiedCalled {
		t.Fatal(
			"MarkVerified() was called outside atomic token issuance",
		)
	}

	if challengeRepository.recordFailedAttemptCalled {
		t.Fatal(
			"RecordFailedAttempt() was called for a valid OTP",
		)
	}
}

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

			service := NewService(
				&testChallengeRepository{},
				&testIdentityRepository{},
				&testOTPGenerator{},
				&testOTPHasher{},
				&testOTPDelivery{},
				&testOTPRequestRateLimiter{},
				&testChallengeIDGenerator{},
				&testTokenIssuer{},
				refreshStore,
				&testSessionRevocationStore{},
				&testAllSessionsRevocationStore{},
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
		})
	}
}

func TestRefreshTokenRotatesTokenAndClampsExpirationToSession(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	sessionExpiresAt := now.Add(
		2 * time.Hour,
	)

	refreshStore := &testRefreshTokenRotationStore{
		inspectResult: RefreshTokenContext{
			IdentityID:       "identity-123",
			SessionID:        "session-123",
			SessionExpiresAt: sessionExpiresAt,
		},
	}

	refreshGenerator := &testRefreshTokenGenerator{
		token: "rt_replacement",
	}

	refreshHasher := &testRefreshTokenHasher{}

	accessSigner := &testAccessTokenSigner{
		accessToken:      "new-access-token",
		expiresInSeconds: 900,
	}

	service := NewService(
		&testChallengeRepository{},
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		refreshStore,
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		refreshGenerator,
		refreshHasher,
		accessSigner,
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

	result, err := service.RefreshToken(
		context.Background(),
		RefreshTokenInput{
			RefreshToken: "rt_current",
		},
	)
	if err != nil {
		t.Fatalf(
			"RefreshToken() returned an error: %v",
			err,
		)
	}

	if result.IdentityID != "identity-123" {
		t.Fatalf(
			"IdentityID = %q, expected %q",
			result.IdentityID,
			"identity-123",
		)
	}

	if result.AccessToken != "new-access-token" {
		t.Fatalf(
			"AccessToken = %q, expected %q",
			result.AccessToken,
			"new-access-token",
		)
	}

	if result.RefreshToken != "rt_replacement" {
		t.Fatalf(
			"RefreshToken = %q, expected %q",
			result.RefreshToken,
			"rt_replacement",
		)
	}

	if result.AccessTokenExpiresInSeconds != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds = %d, expected 900",
			result.AccessTokenExpiresInSeconds,
		)
	}

	if refreshStore.inspectCalls != 1 {
		t.Fatalf(
			"Inspect() calls = %d, expected 1",
			refreshStore.inspectCalls,
		)
	}

	if refreshStore.rotateCalls != 1 {
		t.Fatalf(
			"Rotate() calls = %d, expected 1",
			refreshStore.rotateCalls,
		)
	}

	if refreshStore.rotationInput.CurrentTokenHash !=
		"hashed_rt_current" {
		t.Fatalf(
			"CurrentTokenHash = %q, expected %q",
			refreshStore.rotationInput.CurrentTokenHash,
			"hashed_rt_current",
		)
	}

	if refreshStore.rotationInput.ReplacementTokenHash !=
		"hashed_rt_replacement" {
		t.Fatalf(
			"ReplacementTokenHash = %q, expected %q",
			refreshStore.rotationInput.ReplacementTokenHash,
			"hashed_rt_replacement",
		)
	}

	if !refreshStore.rotationInput.ReplacementExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Fatalf(
			"ReplacementExpiresAt = %v, expected %v",
			refreshStore.rotationInput.ReplacementExpiresAt,
			sessionExpiresAt,
		)
	}

	if accessSigner.calls != 1 {
		t.Fatalf(
			"AccessTokenSigner calls = %d, expected 1",
			accessSigner.calls,
		)
	}

	if accessSigner.identityID != "identity-123" {
		t.Fatalf(
			"signed IdentityID = %q, expected %q",
			accessSigner.identityID,
			"identity-123",
		)
	}

	if accessSigner.sessionID != "session-123" {
		t.Fatalf(
			"signed SessionID = %q, expected %q",
			accessSigner.sessionID,
			"session-123",
		)
	}

	if !accessSigner.issuedAt.Equal(now) {
		t.Fatalf(
			"signed issuedAt = %v, expected %v",
			accessSigner.issuedAt,
			now,
		)
	}

	if !accessSigner.sessionExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Fatalf(
			"signed sessionExpiresAt = %v, expected %v",
			accessSigner.sessionExpiresAt,
			sessionExpiresAt,
		)
	}

	if refreshHasher.calls != 2 {
		t.Fatalf(
			"RefreshTokenHasher calls = %d, expected 2",
			refreshHasher.calls,
		)
	}
}

func TestRefreshTokenDoesNotRotateWhenAccessTokenSigningFails(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	refreshStore := &testRefreshTokenRotationStore{
		inspectResult: RefreshTokenContext{
			IdentityID:       "identity-123",
			SessionID:        "session-123",
			SessionExpiresAt: now.Add(24 * time.Hour),
		},
	}

	refreshGenerator := &testRefreshTokenGenerator{
		token: "rt_replacement",
	}

	refreshHasher := &testRefreshTokenHasher{}

	accessSigner := &testAccessTokenSigner{
		err: errors.New("signing failed"),
	}

	service := NewService(
		&testChallengeRepository{},
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		refreshStore,
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		refreshGenerator,
		refreshHasher,
		accessSigner,
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

	_, err := service.RefreshToken(
		context.Background(),
		RefreshTokenInput{
			RefreshToken: "rt_current",
		},
	)
	if err == nil {
		t.Fatal(
			"RefreshToken() returned nil error, expected signing failure",
		)
	}

	if accessSigner.calls != 1 {
		t.Fatalf(
			"AccessTokenSigner calls = %d, expected 1",
			accessSigner.calls,
		)
	}

	if refreshStore.rotateCalls != 0 {
		t.Fatalf(
			"Rotate() calls = %d, expected 0",
			refreshStore.rotateCalls,
		)
	}

	if refreshGenerator.calls != 1 {
		t.Fatalf(
			"RefreshTokenGenerator calls = %d, expected 1",
			refreshGenerator.calls,
		)
	}
}

func TestRefreshTokenReturnsReuseErrorWhenRotationDetectsReuse(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		10,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	refreshStore := &testRefreshTokenRotationStore{
		inspectResult: RefreshTokenContext{
			IdentityID:       "identity-123",
			SessionID:        "session-123",
			SessionExpiresAt: now.Add(24 * time.Hour),
		},
		rotateErr: ErrRefreshTokenReused,
	}

	refreshGenerator := &testRefreshTokenGenerator{
		token: "rt_replacement",
	}

	refreshHasher := &testRefreshTokenHasher{}

	accessSigner := &testAccessTokenSigner{
		accessToken:      "new-access-token",
		expiresInSeconds: 900,
	}

	service := NewService(
		&testChallengeRepository{},
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		refreshStore,
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		refreshGenerator,
		refreshHasher,
		accessSigner,
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

	result, err := service.RefreshToken(
		context.Background(),
		RefreshTokenInput{
			RefreshToken: "rt_current",
		},
	)

	if !errors.Is(
		err,
		ErrRefreshTokenReused,
	) {
		t.Fatalf(
			"RefreshToken() error = %v, expected %v",
			err,
			ErrRefreshTokenReused,
		)
	}

	if result != (RefreshTokenResult{}) {
		t.Fatalf(
			"RefreshToken() result = %+v, expected empty result",
			result,
		)
	}

	if refreshStore.inspectCalls != 1 {
		t.Fatalf(
			"Inspect() calls = %d, expected 1",
			refreshStore.inspectCalls,
		)
	}

	if refreshStore.rotateCalls != 1 {
		t.Fatalf(
			"Rotate() calls = %d, expected 1",
			refreshStore.rotateCalls,
		)
	}

	if accessSigner.calls != 1 {
		t.Fatalf(
			"AccessTokenSigner calls = %d, expected 1",
			accessSigner.calls,
		)
	}
}

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

	service := NewService(
		&testChallengeRepository{},
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		sessionRevocationStore,
		&testAllSessionsRevocationStore{},
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

			service := NewService(
				&testChallengeRepository{},
				&testIdentityRepository{},
				&testOTPGenerator{},
				&testOTPHasher{},
				&testOTPDelivery{},
				&testOTPRequestRateLimiter{},
				&testChallengeIDGenerator{},
				&testTokenIssuer{},
				&testRefreshTokenRotationStore{},
				sessionRevocationStore,
				&testAllSessionsRevocationStore{},
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

	service := NewService(
		&testChallengeRepository{},
		&testIdentityRepository{},
		&testOTPGenerator{},
		&testOTPHasher{},
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		&testTokenIssuer{},
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		allSessionsRevocationStore,
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

			service := NewService(
				&testChallengeRepository{},
				&testIdentityRepository{},
				&testOTPGenerator{},
				&testOTPHasher{},
				&testOTPDelivery{},
				&testOTPRequestRateLimiter{},
				&testChallengeIDGenerator{},
				&testTokenIssuer{},
				&testRefreshTokenRotationStore{},
				&testSessionRevocationStore{},
				allSessionsRevocationStore,
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
