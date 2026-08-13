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

	createdChallenge OTPChallenge

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
	r.createdChallenge = challenge

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

type testIdentityIdentifierRepository struct {
	findResult     Identity
	findFound      bool
	findErr        error
	findCalls      int
	findIdentifier Identifier

	createResult     Identity
	createErr        error
	createCalls      int
	createIdentifier Identifier
	createVerifiedAt time.Time

	linkCalls      int
	linkIdentityID string
	linkIdentifier Identifier
	linkVerifiedAt time.Time
	linkErr        error
}

func (r *testIdentityIdentifierRepository) FindIdentityByIdentifier(
	ctx context.Context,
	identifier Identifier,
) (Identity, bool, error) {
	r.findCalls++
	r.findIdentifier = identifier

	if r.findErr != nil {
		return Identity{}, false, r.findErr
	}

	return r.findResult, r.findFound, nil
}

func (r *testIdentityIdentifierRepository) CreateIdentityWithIdentifier(
	ctx context.Context,
	identifier Identifier,
	verifiedAt time.Time,
) (Identity, error) {
	r.createCalls++
	r.createIdentifier = identifier
	r.createVerifiedAt = verifiedAt

	if r.createErr != nil {
		return Identity{}, r.createErr
	}

	return r.createResult, nil
}

func (r *testIdentityIdentifierRepository) LinkIdentifier(
	ctx context.Context,
	identityID string,
	identifier Identifier,
	verifiedAt time.Time,
) error {
	r.linkCalls++
	r.linkIdentityID = identityID
	r.linkIdentifier = identifier
	r.linkVerifiedAt = verifiedAt

	return r.linkErr
}

type testIdentifierLinkCompletionStore struct {
	calls int
	input IdentifierLinkCompletionInput
	err   error
}

func (s *testIdentifierLinkCompletionStore) Complete(
	ctx context.Context,
	input IdentifierLinkCompletionInput,
) error {
	s.calls++
	s.input = input

	return s.err
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

	recipient string
	code      string
}

func (d *testOTPDelivery) Send(
	ctx context.Context,
	recipient string,
	code string,
) error {
	d.called = true
	d.recipient = recipient
	d.code = code

	if d.onSend != nil {
		d.onSend()
	}

	return d.err
}

type testOTPRequestRateLimiter struct {
	called bool
	err    error

	identifierValue string
}

func (r *testOTPRequestRateLimiter) Allow(
	ctx context.Context,
	scope OTPRequestScope,
	now time.Time,
	policy OTPRequestRateLimitPolicy,
) error {
	r.called = true
	r.identifierValue = scope.Identifier.Value

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
	challengeRepository           ChallengeRepository
	identityIdentifierRepository  IdentityIdentifierRepository
	identifierLinkCompletionStore IdentifierLinkCompletionStore
	otpGenerator                  OTPGenerator
	otpHasher                     OTPHasher
	otpDelivery                   OTPDelivery
	otpRequestRateLimiter         OTPRequestRateLimiter
	challengeIDGenerator          ChallengeIDGenerator
	tokenIssuer                   TokenIssuer
	refreshTokenRotationStore     RefreshTokenRotationStore
	sessionRevocationStore        SessionRevocationStore
	allSessionsRevocationStore    AllSessionsRevocationStore
	refreshTokenGenerator         RefreshTokenGenerator
	refreshTokenHasher            RefreshTokenHasher
	accessTokenSigner             AccessTokenSigner
	clock                         Clock

	otpTTL                    time.Duration
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy
	refreshTokenTTL           time.Duration
}

func newValidServiceConstructorTestDependencies() serviceConstructorTestDependencies {
	return serviceConstructorTestDependencies{
		challengeRepository:           &testChallengeRepository{},
		identityIdentifierRepository:  &testIdentityIdentifierRepository{},
		identifierLinkCompletionStore: &testIdentifierLinkCompletionStore{},
		otpGenerator:                  &testOTPGenerator{},
		otpHasher:                     &testOTPHasher{},
		otpDelivery:                   &testOTPDelivery{},
		otpRequestRateLimiter:         &testOTPRequestRateLimiter{},
		challengeIDGenerator:          &testChallengeIDGenerator{},
		tokenIssuer:                   &testTokenIssuer{},
		refreshTokenRotationStore:     &testRefreshTokenRotationStore{},
		sessionRevocationStore:        &testSessionRevocationStore{},
		allSessionsRevocationStore:    &testAllSessionsRevocationStore{},
		refreshTokenGenerator:         &testRefreshTokenGenerator{},
		refreshTokenHasher:            &testRefreshTokenHasher{},
		accessTokenSigner:             &testAccessTokenSigner{},
		clock:                         &testClock{},

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
	return NewServiceWithIdentityIdentifiers(
		dependencies.challengeRepository,
		dependencies.identityIdentifierRepository,
		dependencies.identifierLinkCompletionStore,
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

func newIdentifierAwareServiceForTest(
	challengeRepository ChallengeRepository,
	identityIdentifierRepository IdentityIdentifierRepository,
	identifierLinkCompletionStore IdentifierLinkCompletionStore,
	otpHasher OTPHasher,
	tokenIssuer TokenIssuer,
	clock Clock,
) Service {
	return NewServiceWithIdentityIdentifiers(
		challengeRepository,
		identityIdentifierRepository,
		identifierLinkCompletionStore,
		&testOTPGenerator{},
		otpHasher,
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
		clock,
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
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
			name: "nil identity identifier repository",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identityIdentifierRepository = nil
			},
		},
		{
			name: "nil identifier link completion store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identifierLinkCompletionStore = nil
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
						"NewServiceWithIdentityIdentifiers() did not panic for invalid configuration",
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

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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
}

func TestRequestOTPCancelsChallengeWhenDeliveryFails(
	t *testing.T,
) {
	deliveryError := errors.New("SMS provider unavailable")

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{
		err: deliveryError,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
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

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
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

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose: OTPPurposeLogin,
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

func TestRequestOTPUsesGenericPhoneLoginIdentifier(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies.challengeRepository = challengeRepository
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter
	dependencies.clock = &testClock{
		now: time.Date(
			2026,
			time.August,
			13,
			8,
			0,
			0,
			0,
			time.UTC,
		),
	}

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.RequestOTP(
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

	expectedIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647501234567",
	}

	if challengeRepository.createdChallenge.Identifier !=
		expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challengeRepository.createdChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if challengeRepository.createdChallenge.Purpose !=
		OTPPurposeLogin {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challengeRepository.createdChallenge.Purpose,
			OTPPurposeLogin,
		)
	}

	if challengeRepository.createdChallenge.TargetIdentityID != nil {
		t.Fatal(
			"login challenge unexpectedly has target identity",
		)
	}

	if rateLimiter.identifierValue !=
		expectedIdentifier.Value {
		t.Fatalf(
			"rate limiter identifier = %q, expected %q",
			rateLimiter.identifierValue,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.recipient != expectedIdentifier.Value {
		t.Fatalf(
			"OTP delivery recipient = %q, expected %q",
			otpDelivery.recipient,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.code != "123456" {
		t.Fatalf(
			"OTP delivery code = %q, expected %q",
			otpDelivery.code,
			"123456",
		)
	}
}

func TestRequestOTPUsesNormalizedEmailLoginIdentifier(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies.challengeRepository = challengeRepository
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "  User.Name@EXAMPLE.COM  ",
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

	expectedIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "user.name@example.com",
	}

	if challengeRepository.createdChallenge.Identifier !=
		expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challengeRepository.createdChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if challengeRepository.createdChallenge.Purpose !=
		OTPPurposeLogin {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challengeRepository.createdChallenge.Purpose,
			OTPPurposeLogin,
		)
	}

	if challengeRepository.createdChallenge.TargetIdentityID != nil {
		t.Fatal(
			"email login challenge unexpectedly has target identity",
		)
	}

	if rateLimiter.identifierValue !=
		expectedIdentifier.Value {
		t.Fatalf(
			"rate limiter identifier = %q, expected %q",
			rateLimiter.identifierValue,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.recipient != expectedIdentifier.Value {
		t.Fatalf(
			"OTP delivery recipient = %q, expected %q",
			otpDelivery.recipient,
			expectedIdentifier.Value,
		)
	}
}

func TestRequestOTPUsesLinkIdentifierScope(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}

	dependencies.challengeRepository =
		challengeRepository

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	targetIdentityID :=
		"  11111111-1111-1111-1111-111111111111  "

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "Link.Me@EXAMPLE.COM",
			},
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &targetIdentityID,
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	challenge :=
		challengeRepository.createdChallenge

	expectedIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "link.me@example.com",
	}

	if challenge.Identifier != expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challenge.Identifier,
			expectedIdentifier,
		)
	}

	if challenge.Purpose !=
		OTPPurposeLinkIdentifier {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challenge.Purpose,
			OTPPurposeLinkIdentifier,
		)
	}

	if challenge.TargetIdentityID == nil {
		t.Fatal(
			"link identifier challenge has nil target identity",
		)
	}

	expectedIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	if *challenge.TargetIdentityID !=
		expectedIdentityID {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			*challenge.TargetIdentityID,
			expectedIdentityID,
		)
	}
}

func TestRequestOTPRejectsInvalidGenericScopeBeforeSideEffects(
	t *testing.T,
) {
	targetIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name  string
		input RequestOTPInput
	}{
		{
			name: "blank generic identifier value",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "   ",
				},
				Purpose: OTPPurposeLogin,
			},
		},
		{
			name: "login cannot target identity",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose:          OTPPurposeLogin,
				TargetIdentityID: &targetIdentityID,
			},
		},
		{
			name: "link identifier requires target identity",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose: OTPPurposeLinkIdentifier,
			},
		},
		{
			name: "generic request requires valid purpose",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
			},
		},
		{
			name: "invalid purpose",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose: OTPPurpose("password_reset"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependencies :=
				newValidServiceConstructorTestDependencies()

			challengeRepository :=
				&testChallengeRepository{}

			otpGenerator := &testOTPGenerator{}
			otpDelivery := &testOTPDelivery{}
			rateLimiter :=
				&testOTPRequestRateLimiter{}

			dependencies.challengeRepository =
				challengeRepository
			dependencies.otpGenerator =
				otpGenerator
			dependencies.otpDelivery =
				otpDelivery
			dependencies.otpRequestRateLimiter =
				rateLimiter

			service :=
				newServiceFromConstructorTestDependencies(
					dependencies,
				)

			_, err := service.RequestOTP(
				context.Background(),
				tt.input,
			)

			if err == nil {
				t.Fatal(
					"RequestOTP() accepted invalid generic OTP scope",
				)
			}

			if rateLimiter.called {
				t.Fatal(
					"rate limiter was called for invalid OTP request",
				)
			}

			if otpGenerator.called {
				t.Fatal(
					"OTP generator was called for invalid OTP request",
				)
			}

			if challengeRepository.createCalled {
				t.Fatal(
					"challenge was created for invalid OTP request",
				)
			}

			if otpDelivery.called {
				t.Fatal(
					"OTP delivery was called for invalid OTP request",
				)
			}
		})
	}
}

func TestVerifyOTPLogsInExistingIdentityByEmail(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		9,
		0,
		0,
		0,
		time.UTC,
	)

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "existing@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:         "otp_ch_email_existing",
			Identifier: identifier,
			Purpose:    OTPPurposeLogin,
			CodeHash:   "hashed-code",
			ExpiresAt:  now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_existing",
			IsActive: true,
		},
		findFound: true,
	}

	linkStore := &testIdentifierLinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{
		result: TokenPair{
			AccessToken:                 "access-existing",
			RefreshToken:                "refresh-existing",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		linkStore,
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_email_existing",
			Code:            "123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if identityRepository.findCalls != 1 {
		t.Fatalf(
			"FindIdentityByIdentifier() calls = %d, expected 1",
			identityRepository.findCalls,
		)
	}

	if identityRepository.findIdentifier != identifier {
		t.Fatalf(
			"searched identifier = %+v, expected %+v",
			identityRepository.findIdentifier,
			identifier,
		)
	}

	if identityRepository.createCalls != 0 {
		t.Fatal(
			"existing identity caused CreateIdentityWithIdentifier()",
		)
	}

	if tokenIssuer.calls != 1 {
		t.Fatalf(
			"TokenIssuer calls = %d, expected 1",
			tokenIssuer.calls,
		)
	}

	if tokenIssuer.input.Identity.ID != "identity_existing" {
		t.Fatalf(
			"issued identity ID = %q, expected %q",
			tokenIssuer.input.Identity.ID,
			"identity_existing",
		)
	}

	if tokenIssuer.input.ChallengeID !=
		"otp_ch_email_existing" {
		t.Fatalf(
			"issued challenge ID = %q, expected %q",
			tokenIssuer.input.ChallengeID,
			"otp_ch_email_existing",
		)
	}

	if linkStore.calls != 0 {
		t.Fatal(
			"login invoked identifier link completion store",
		)
	}

	if result.IdentityID != "identity_existing" {
		t.Fatalf(
			"result identity ID = %q, expected %q",
			result.IdentityID,
			"identity_existing",
		)
	}

	if result.AccessToken != "access-existing" {
		t.Fatalf(
			"access token = %q, expected %q",
			result.AccessToken,
			"access-existing",
		)
	}

	if result.RefreshToken != "refresh-existing" {
		t.Fatalf(
			"refresh token = %q, expected %q",
			result.RefreshToken,
			"refresh-existing",
		)
	}
}

func TestVerifyOTPCreatesIdentityForUnknownEmail(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		9,
		30,
		0,
		0,
		time.UTC,
	)

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "new@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:         "otp_ch_email_new",
			Identifier: identifier,
			Purpose:    OTPPurposeLogin,
			CodeHash:   "hashed-code",
			ExpiresAt:  now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository := &testIdentityIdentifierRepository{
		findFound: false,
		createResult: Identity{
			ID:       "identity_new",
			IsActive: true,
		},
	}

	tokenIssuer := &testTokenIssuer{
		result: TokenPair{
			AccessToken:                 "access-new",
			RefreshToken:                "refresh-new",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_email_new",
			Code:            "123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if identityRepository.findCalls != 1 {
		t.Fatalf(
			"FindIdentityByIdentifier() calls = %d, expected 1",
			identityRepository.findCalls,
		)
	}

	if identityRepository.createCalls != 1 {
		t.Fatalf(
			"CreateIdentityWithIdentifier() calls = %d, expected 1",
			identityRepository.createCalls,
		)
	}

	if identityRepository.createIdentifier != identifier {
		t.Fatalf(
			"created identifier = %+v, expected %+v",
			identityRepository.createIdentifier,
			identifier,
		)
	}

	if !identityRepository.createVerifiedAt.Equal(now) {
		t.Fatalf(
			"identifier verified at %v, expected %v",
			identityRepository.createVerifiedAt,
			now,
		)
	}

	if tokenIssuer.calls != 1 {
		t.Fatalf(
			"TokenIssuer calls = %d, expected 1",
			tokenIssuer.calls,
		)
	}

	if tokenIssuer.input.Identity.ID != "identity_new" {
		t.Fatalf(
			"issued identity ID = %q, expected %q",
			tokenIssuer.input.Identity.ID,
			"identity_new",
		)
	}

	if result.IdentityID != "identity_new" {
		t.Fatalf(
			"result identity ID = %q, expected %q",
			result.IdentityID,
			"identity_new",
		)
	}
}

func TestVerifyOTPRejectsInactiveIdentifierIdentityBeforeIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "inactive@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:         "otp_ch_email_inactive",
			Identifier: identifier,
			Purpose:    OTPPurposeLogin,
			CodeHash:   "hashed-code",
			ExpiresAt:  now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: Identity{
			ID:       "identity_inactive",
			IsActive: false,
		},
		findFound: true,
	}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentifierLinkCompletionStore{},
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_email_inactive",
			Code:            "123456",
		},
	)

	if !errors.Is(err, ErrIdentityInactive) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrIdentityInactive,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"inactive identity caused token issuance",
		)
	}
}

func TestVerifyOTPCompletesIdentifierLinkWithoutIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		10,
		30,
		0,
		0,
		time.UTC,
	)

	identityID :=
		"11111111-1111-1111-1111-111111111111"

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "linked@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:               "otp_ch_link_email",
			Identifier:       identifier,
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &identityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository :=
		&testIdentityIdentifierRepository{}

	linkStore :=
		&testIdentifierLinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		linkStore,
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &identityID,
			ChallengeID:              "otp_ch_link_email",
			Code:                     "123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if linkStore.calls != 1 {
		t.Fatalf(
			"IdentifierLinkCompletionStore calls = %d, expected 1",
			linkStore.calls,
		)
	}

	expectedInput := IdentifierLinkCompletionInput{
		ChallengeID: "otp_ch_link_email",
		IdentityID:  identityID,
		Identifier:  identifier,
		VerifiedAt:  now,
	}

	if linkStore.input.ChallengeID !=
		expectedInput.ChallengeID {
		t.Fatalf(
			"link challenge ID = %q, expected %q",
			linkStore.input.ChallengeID,
			expectedInput.ChallengeID,
		)
	}

	if linkStore.input.IdentityID !=
		expectedInput.IdentityID {
		t.Fatalf(
			"link identity ID = %q, expected %q",
			linkStore.input.IdentityID,
			expectedInput.IdentityID,
		)
	}

	if linkStore.input.Identifier !=
		expectedInput.Identifier {
		t.Fatalf(
			"link identifier = %+v, expected %+v",
			linkStore.input.Identifier,
			expectedInput.Identifier,
		)
	}

	if !linkStore.input.VerifiedAt.Equal(
		expectedInput.VerifiedAt,
	) {
		t.Fatalf(
			"link verification time = %v, expected %v",
			linkStore.input.VerifiedAt,
			expectedInput.VerifiedAt,
		)
	}

	if identityRepository.findCalls != 0 ||
		identityRepository.createCalls != 0 ||
		identityRepository.linkCalls != 0 {
		t.Fatal(
			"link_identifier used IdentityIdentifierRepository directly",
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"link_identifier issued authentication tokens",
		)
	}

	if result.IdentityID != identityID {
		t.Fatalf(
			"result identity ID = %q, expected %q",
			result.IdentityID,
			identityID,
		)
	}

	if result.AccessToken != "" ||
		result.RefreshToken != "" ||
		result.AccessTokenExpiresInSeconds != 0 {
		t.Fatal(
			"link_identifier returned authentication tokens",
		)
	}
}

func TestVerifyOTPRejectsIdentifierLinkForDifferentAuthenticatedIdentity(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		11,
		0,
		0,
		0,
		time.UTC,
	)

	challengeTargetIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	authenticatedIdentityID :=
		"22222222-2222-2222-2222-222222222222"

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_wrong_identity",
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "linked@example.com",
			},
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &challengeTargetIdentityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),
			FailedAttempts:   0,
			MaxAttempts:      5,
		},
	}

	otpHasher := &testOTPHasher{}

	linkStore :=
		&testIdentifierLinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		linkStore,
		otpHasher,
		tokenIssuer,
		&testClock{now: now},
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &authenticatedIdentityID,
			ChallengeID:              "otp_ch_wrong_identity",
			Code:                     "123456",
		},
	)

	if !errors.Is(
		err,
		ErrOTPChallengeTargetMismatch,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrOTPChallengeTargetMismatch,
		)
	}

	if otpHasher.compareCalled {
		t.Fatal(
			"OTP code was compared before target identity validation",
		)
	}

	if linkStore.calls != 0 {
		t.Fatalf(
			"IdentifierLinkCompletionStore calls = %d, expected 0",
			linkStore.calls,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"authentication tokens were issued for mismatched target identity",
		)
	}
}

func TestVerifyOTPMapsIdentifierAlreadyLinkedWithoutIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		11,
		0,
		0,
		0,
		time.UTC,
	)

	identityID :=
		"22222222-2222-2222-2222-222222222222"

	identifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000077",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:               "otp_ch_link_conflict",
			Identifier:       identifier,
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &identityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	linkStore := &testIdentifierLinkCompletionStore{
		err: ErrIdentifierAlreadyLinked,
	}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		linkStore,
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &identityID,
			ChallengeID:              "otp_ch_link_conflict",
			Code:                     "123456",
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

	if linkStore.calls != 1 {
		t.Fatalf(
			"IdentifierLinkCompletionStore calls = %d, expected 1",
			linkStore.calls,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"identifier ownership conflict issued tokens",
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
			ID: "otp_ch_test",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose:        OTPPurposeLogin,
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

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_test",
			Code:            "000000",
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
			ID: "otp_ch_test",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose:        OTPPurposeLogin,
			CodeHash:       "corrupted-hash",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	otpHasher := &testOTPHasher{
		compareErr: compareError,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_test",
			Code:            "123456",
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
			ID: "otp_ch_test",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647501234567",
			},
			Purpose:        OTPPurposeLogin,
			CodeHash:       "hashed-code",
			ExpiresAt:      now.Add(5 * time.Minute),
			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identity := Identity{
		ID:       "identity-123",
		IsActive: true,
	}

	identityRepository := &testIdentityIdentifierRepository{
		findResult: identity,
		findFound:  true,
	}

	tokenIssuer := &testTokenIssuer{
		err: ErrChallengeCancelled,
	}

	service := NewServiceWithIdentityIdentifiers(
		challengeRepository,
		identityRepository,
		&testIdentifierLinkCompletionStore{},
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
			ExpectedPurpose: OTPPurposeLogin,
			ChallengeID:     "otp_ch_test",
			Code:            "123456",
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

			service := NewServiceWithIdentityIdentifiers(
				&testChallengeRepository{},
				&testIdentityIdentifierRepository{},
				&testIdentifierLinkCompletionStore{},
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

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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

			service := NewServiceWithIdentityIdentifiers(
				&testChallengeRepository{},
				&testIdentityIdentifierRepository{},
				&testIdentifierLinkCompletionStore{},
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

	service := NewServiceWithIdentityIdentifiers(
		&testChallengeRepository{},
		&testIdentityIdentifierRepository{},
		&testIdentifierLinkCompletionStore{},
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

			service := NewServiceWithIdentityIdentifiers(
				&testChallengeRepository{},
				&testIdentityIdentifierRepository{},
				&testIdentifierLinkCompletionStore{},
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
