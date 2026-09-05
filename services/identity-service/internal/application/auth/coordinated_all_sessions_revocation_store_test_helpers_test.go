package auth

import (
	"context"
	"time"
)

type testAllSessionsRevocationTargetStore struct {
	target           AllSessionsRevocationTarget
	found            bool
	err              error
	called           bool
	refreshTokenHash string
	now              time.Time
	callOrder        *[]string
}

func (s *testAllSessionsRevocationTargetStore) FindAllSessionRevocationTargetsByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	now time.Time,
) (AllSessionsRevocationTarget, bool, error) {
	s.called = true
	s.refreshTokenHash = refreshTokenHash
	s.now = now

	if s.callOrder != nil {
		*s.callOrder = append(
			*s.callOrder,
			"target",
		)
	}

	return s.target, s.found, s.err
}

type testAllSessionsAccessRevocationStore struct {
	calls      []testAllSessionsAccessRevocationCall
	failOnCall int
	err        error
	callOrder  *[]string
}

type testAllSessionsAccessRevocationCall struct {
	sessionID string
	ttl       time.Duration
}

func (s *testAllSessionsAccessRevocationStore) MarkRevoked(
	ctx context.Context,
	sessionID string,
	ttl time.Duration,
) error {
	s.calls = append(
		s.calls,
		testAllSessionsAccessRevocationCall{
			sessionID: sessionID,
			ttl:       ttl,
		},
	)

	if s.callOrder != nil {
		*s.callOrder = append(
			*s.callOrder,
			"access:"+sessionID,
		)
	}

	if s.failOnCall > 0 &&
		len(s.calls) == s.failOnCall {
		return s.err
	}

	return nil
}

func (s *testAllSessionsAccessRevocationStore) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	return false, nil
}

type testAllSessionsPersistentRevocationStore struct {
	called     bool
	identityID string
	sessionIDs []string
	revokedAt  time.Time
	err        error
	callOrder  *[]string
}

func (s *testAllSessionsPersistentRevocationStore) RevokeSessions(
	ctx context.Context,
	identityID string,
	sessionIDs []string,
	revokedAt time.Time,
) error {
	s.called = true
	s.identityID = identityID
	s.sessionIDs = append(
		[]string(nil),
		sessionIDs...,
	)
	s.revokedAt = revokedAt

	if s.callOrder != nil {
		*s.callOrder = append(
			*s.callOrder,
			"persistent",
		)
	}

	return s.err
}
