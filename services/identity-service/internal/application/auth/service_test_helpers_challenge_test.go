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
