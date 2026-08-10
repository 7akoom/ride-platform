package auth

import (
	"context"
	"time"
)

type OTPChallenge struct {
	ID             string
	PhoneNumber    string
	CodeHash       string
	ExpiresAt      time.Time
	VerifiedAt     *time.Time
	CancelledAt    *time.Time
	FailedAttempts int16
	MaxAttempts    int16
}

type Identity struct {
	ID          string
	PhoneNumber string
	IsActive    bool
}

type ChallengeRepository interface {
	Create(
		ctx context.Context,
		challenge OTPChallenge,
	) error

	FindByID(
		ctx context.Context,
		challengeID string,
	) (OTPChallenge, error)

	RecordFailedAttempt(
		ctx context.Context,
		challengeID string,
		attemptedAt time.Time,
	) error

	MarkVerified(
		ctx context.Context,
		challengeID string,
		verifiedAt time.Time,
	) error

	Cancel(
		ctx context.Context,
		challengeID string,
		cancelledAt time.Time,
	) error
}

type IdentityRepository interface {
	FindOrCreateByPhoneNumber(
		ctx context.Context,
		phoneNumber string,
	) (Identity, error)
}

type OTPGenerator interface {
	Generate() (string, error)
}

type OTPHasher interface {
	Hash(code string) (string, error)

	Compare(
		hash string,
		code string,
	) error
}

type OTPDelivery interface {
	Send(
		ctx context.Context,
		phoneNumber string,
		code string,
	) error
}

type TokenPair struct {
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type TokenIssuer interface {
	Issue(
		ctx context.Context,
		identity Identity,
	) (TokenPair, error)
}

type ChallengeIDGenerator interface {
	Generate() (string, error)
}

type Clock interface {
	Now() time.Time
}

type OTPRequestRateLimitPolicy struct {
	Cooldown   time.Duration
	Window     time.Duration
	MaxRequests int
}

type OTPRequestRateLimiter interface {
	Allow(
		ctx context.Context,
		phoneNumber string,
		now time.Time,
		policy OTPRequestRateLimitPolicy,
	) error
}

type RefreshTokenContext struct {
	IdentityID       string
	SessionID        string
	SessionExpiresAt time.Time
}

type RefreshTokenRotationInput struct {
	CurrentTokenHash      string
	ReplacementTokenHash  string
	RotatedAt             time.Time
	ReplacementExpiresAt  time.Time
}

type RefreshTokenRotationStore interface {
	Inspect(
		ctx context.Context,
		currentTokenHash string,
		now time.Time,
	) (RefreshTokenContext, error)

	Rotate(
		ctx context.Context,
		input RefreshTokenRotationInput,
	) error
}

type RefreshTokenGenerator interface {
	Generate() (string, error)
}

type RefreshTokenHasher interface {
	Hash(refreshToken string) string
}

type AccessTokenSigner interface {
	Issue(
		identityID string,
		sessionID string,
		issuedAt time.Time,
	) (string, int32, error)
}