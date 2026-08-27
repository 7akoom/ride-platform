package auth

import (
	"context"
	"time"
)

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
	purpose   OTPPurpose
}

func (d *testOTPDelivery) Send(
	ctx context.Context,
	input OTPDeliveryInput,
) error {
	d.called = true
	d.recipient = input.Identifier
	d.code = input.Code
	d.purpose = input.Purpose

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
