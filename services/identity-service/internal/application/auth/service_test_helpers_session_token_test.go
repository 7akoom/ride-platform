package auth

import (
	"context"
	"time"
)

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
