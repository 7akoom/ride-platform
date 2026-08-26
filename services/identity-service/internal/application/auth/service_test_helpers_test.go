package auth

import (
	"context"
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

type testIdentityReader struct {
	findResult     IdentityDetails
	findFound      bool
	findErr        error
	findCalls      int
	findIdentityID string
}

func (r *testIdentityReader) FindByID(
	ctx context.Context,
	identityID string,
) (IdentityDetails, bool, error) {
	r.findCalls++
	r.findIdentityID = identityID

	if r.findErr != nil {
		return IdentityDetails{}, false, r.findErr
	}

	return r.findResult, r.findFound, nil
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

type testIdentifierUnlinkRequestStore struct {
	calls int
	input IdentifierUnlinkRequestInput
	err   error
}

func (s *testIdentifierUnlinkRequestStore) Create(
	ctx context.Context,
	input IdentifierUnlinkRequestInput,
) error {
	s.calls++
	s.input = input

	return s.err
}

type testIdentifierUnlinkCompletionStore struct {
	calls int
	input IdentifierUnlinkCompletionInput
	err   error
}

func (s *testIdentifierUnlinkCompletionStore) Complete(
	ctx context.Context,
	input IdentifierUnlinkCompletionInput,
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

	recipient Identifier
	code      string
}

func (d *testOTPDelivery) Send(
	ctx context.Context,
	recipient Identifier,
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
	scope           OTPRequestScope
}

func (r *testOTPRequestRateLimiter) Allow(
	ctx context.Context,
	scope OTPRequestScope,
	now time.Time,
	policy OTPRequestRateLimitPolicy,
) error {
	r.called = true
	r.identifierValue = scope.Identifier.Value
	r.scope = scope

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

type testSessionReader struct {
	called bool

	identityID string
	now        time.Time

	sessions []SessionDetails
	err      error
}

func (r *testSessionReader) ListActiveByIdentity(
	ctx context.Context,
	identityID string,
	now time.Time,
) ([]SessionDetails, error) {
	r.called = true
	r.identityID = identityID
	r.now = now

	return r.sessions, r.err
}

type testSessionManagementRevocationStore struct {
	called bool

	identityID string
	sessionID  string
	revokedAt  time.Time

	err error
}

func (s *testSessionManagementRevocationStore) RevokeSession(
	ctx context.Context,
	identityID string,
	sessionID string,
	revokedAt time.Time,
) error {
	s.called = true
	s.identityID = identityID
	s.sessionID = sessionID
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
	challengeRepository              ChallengeRepository
	identityIdentifierRepository     IdentityIdentifierRepository
	identityReader                   IdentityReader
	identifierLinkCompletionStore    IdentifierLinkCompletionStore
	identifierUnlinkRequestStore     IdentifierUnlinkRequestStore
	identifierUnlinkCompletionStore  IdentifierUnlinkCompletionStore
	otpGenerator                     OTPGenerator
	otpHasher                        OTPHasher
	otpDelivery                      OTPDelivery
	otpRequestRateLimiter            OTPRequestRateLimiter
	challengeIDGenerator             ChallengeIDGenerator
	tokenIssuer                      TokenIssuer
	refreshTokenRotationStore        RefreshTokenRotationStore
	sessionRevocationStore           SessionRevocationStore
	allSessionsRevocationStore       AllSessionsRevocationStore
	sessionReader                    SessionReader
	sessionManagementRevocationStore SessionManagementRevocationStore
	refreshTokenGenerator            RefreshTokenGenerator
	refreshTokenHasher               RefreshTokenHasher
	accessTokenSigner                AccessTokenSigner
	clock                            Clock

	otpTTL                    time.Duration
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy
	refreshTokenTTL           time.Duration
}

func newValidServiceConstructorTestDependencies() serviceConstructorTestDependencies {
	return serviceConstructorTestDependencies{
		challengeRepository:              &testChallengeRepository{},
		identityIdentifierRepository:     &testIdentityIdentifierRepository{},
		identityReader:                   &testIdentityReader{},
		identifierLinkCompletionStore:    &testIdentifierLinkCompletionStore{},
		identifierUnlinkRequestStore:     &testIdentifierUnlinkRequestStore{},
		identifierUnlinkCompletionStore:  &testIdentifierUnlinkCompletionStore{},
		otpGenerator:                     &testOTPGenerator{},
		otpHasher:                        &testOTPHasher{},
		otpDelivery:                      &testOTPDelivery{},
		otpRequestRateLimiter:            &testOTPRequestRateLimiter{},
		challengeIDGenerator:             &testChallengeIDGenerator{},
		tokenIssuer:                      &testTokenIssuer{},
		refreshTokenRotationStore:        &testRefreshTokenRotationStore{},
		sessionRevocationStore:           &testSessionRevocationStore{},
		allSessionsRevocationStore:       &testAllSessionsRevocationStore{},
		sessionReader:                    &testSessionReader{},
		sessionManagementRevocationStore: &testSessionManagementRevocationStore{},
		refreshTokenGenerator:            &testRefreshTokenGenerator{},
		refreshTokenHasher:               &testRefreshTokenHasher{},
		accessTokenSigner:                &testAccessTokenSigner{},
		clock:                            &testClock{},

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
		dependencies.identityReader,
		dependencies.identifierLinkCompletionStore,
		dependencies.identifierUnlinkRequestStore,
		dependencies.identifierUnlinkCompletionStore,
		dependencies.otpGenerator,
		dependencies.otpHasher,
		dependencies.otpDelivery,
		dependencies.otpRequestRateLimiter,
		dependencies.challengeIDGenerator,
		dependencies.tokenIssuer,
		dependencies.refreshTokenRotationStore,
		dependencies.sessionRevocationStore,
		dependencies.allSessionsRevocationStore,
		dependencies.sessionReader,
		dependencies.sessionManagementRevocationStore,
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
	identityReader IdentityReader,
	identifierLinkCompletionStore IdentifierLinkCompletionStore,
	otpHasher OTPHasher,
	tokenIssuer TokenIssuer,
	clock Clock,
) Service {
	return NewServiceWithIdentityIdentifiers(
		challengeRepository,
		identityIdentifierRepository,
		identityReader,
		identifierLinkCompletionStore,
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		otpHasher,
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		tokenIssuer,
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
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
