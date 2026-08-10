package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testChallengeRepository struct {
	createCalled bool
	cancelCalled bool

	cancelledChallengeID string
	cancelledAt          time.Time
	cancelErr             error
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
	return OTPChallenge{}, nil
}

func (r *testChallengeRepository) RecordFailedAttempt(
	ctx context.Context,
	challengeID string,
	attemptedAt time.Time,
) error {
	return nil
}

func (r *testChallengeRepository) MarkVerified(
	ctx context.Context,
	challengeID string,
	verifiedAt time.Time,
) error {
	return nil
}

func (r *testChallengeRepository) Cancel(
	ctx context.Context,
	challengeID string,
	cancelledAt time.Time,
) error {
	r.cancelCalled = true
	r.cancelledChallengeID = challengeID
	r.cancelledAt = cancelledAt

	return r.cancelErr
}

type testIdentityRepository struct{}

func (r *testIdentityRepository) FindOrCreateByPhoneNumber(
	ctx context.Context,
	phoneNumber string,
) (Identity, error) {
	return Identity{}, nil
}

type testOTPGenerator struct {
	called bool
}

func (g *testOTPGenerator) Generate() (string, error) {
	g.called = true
	return "123456", nil
}

type testOTPHasher struct {
	hashCalled bool
}

func (h *testOTPHasher) Hash(
	code string,
) (string, error) {
	h.hashCalled = true
	return "hashed-code", nil
}

func (h *testOTPHasher) Compare(
	hash string,
	code string,
) error {
	return nil
}

type testOTPDelivery struct {
	called bool
	err    error
}

func (d *testOTPDelivery) Send(
	ctx context.Context,
	phoneNumber string,
	code string,
) error {
	d.called = true

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

type testTokenIssuer struct{}

func (i *testTokenIssuer) Issue(
	ctx context.Context,
	identity Identity,
) (TokenPair, error) {
	return TokenPair{}, nil
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

	identityID string
	sessionID  string
	issuedAt   time.Time
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

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
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
		29 * 24 * time.Hour,
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
		29 * 24 * time.Hour,
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
		29 * 24 * time.Hour,
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

func TestLogoutRejectsEmptyRefreshToken(
	t *testing.T,
) {
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
			RefreshToken: "",
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

func TestLogoutAllSessionsRejectsEmptyRefreshToken(
	t *testing.T,
) {
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
			RefreshToken: "",
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
}