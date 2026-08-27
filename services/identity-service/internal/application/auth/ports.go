package auth

import (
	"context"
	"time"
)

type OTPChallenge struct {
	ID               string
	Identifier       Identifier
	Purpose          OTPPurpose
	TargetIdentityID *string

	CodeHash       string
	ExpiresAt      time.Time
	VerifiedAt     *time.Time
	CancelledAt    *time.Time
	FailedAttempts int16
	MaxAttempts    int16
}

type Identity struct {
	ID       string
	IsActive bool
}

type IdentityIdentifier struct {
	ID         string
	IdentityID string
	Identifier Identifier
	VerifiedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type IdentityDetailsIdentifier struct {
	Identifier Identifier
	VerifiedAt time.Time
}

type IdentityDetails struct {
	ID          string
	Status      IdentityStatus
	Identifiers []IdentityDetailsIdentifier
}

type IdentityReader interface {
	FindByID(
		ctx context.Context,
		identityID string,
	) (IdentityDetails, bool, error)
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

type IdentityIdentifierRepository interface {
	FindIdentityByIdentifier(
		ctx context.Context,
		identifier Identifier,
	) (Identity, bool, error)

	CreateIdentityWithIdentifier(
		ctx context.Context,
		identifier Identifier,
		verifiedAt time.Time,
	) (Identity, error)

	LinkIdentifier(
		ctx context.Context,
		identityID string,
		identifier Identifier,
		verifiedAt time.Time,
	) error
}

type IdentifierLinkCompletionInput struct {
	ChallengeID string
	IdentityID  string
	Identifier  Identifier
	VerifiedAt  time.Time
}

type IdentifierLinkCompletionStore interface {
	Complete(
		ctx context.Context,
		input IdentifierLinkCompletionInput,
	) error
}

type IdentifierUnlinkRequestInput struct {
	Challenge        OTPChallenge
	TargetIdentifier Identifier
}

type IdentifierUnlinkRequestStore interface {
	Create(
		ctx context.Context,
		input IdentifierUnlinkRequestInput,
	) error
}

type IdentifierUnlinkCompletionInput struct {
	ChallengeID string
	IdentityID  string
	VerifiedAt  time.Time
}

type IdentifierUnlinkCompletionStore interface {
	Complete(
		ctx context.Context,
		input IdentifierUnlinkCompletionInput,
	) error
}

type OTPGenerator interface {
	Generate() (string, error)
}

type OTPHasher interface {
	Hash(
		challengeID string,
		code string,
	) (string, error)

	Compare(
		hash string,
		challengeID string,
		code string,
	) (bool, error)
}

type OTPDeliveryInput struct {
	Identifier Identifier
	Code       string
	Purpose    OTPPurpose
	Locale     string
}

type OTPDelivery interface {
	Send(
		ctx context.Context,
		input OTPDeliveryInput,
	) error
}

type TokenPair struct {
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type TokenIssueInput struct {
	Identity        Identity
	ChallengeID     string
	VerifiedAt      time.Time
	SessionMetadata SessionMetadata
}

type TokenIssuer interface {
	Issue(
		ctx context.Context,
		input TokenIssueInput,
	) (TokenPair, error)
}

type ChallengeIDGenerator interface {
	Generate() (string, error)
}

type Clock interface {
	Now() time.Time
}

type OTPRequestRateLimitPolicy struct {
	Cooldown    time.Duration
	Window      time.Duration
	MaxRequests int
}

type OTPRequestScope struct {
	Identifier       Identifier
	Purpose          OTPPurpose
	TargetIdentityID *string
}

type OTPRequestRateLimiter interface {
	Allow(
		ctx context.Context,
		scope OTPRequestScope,
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
	CurrentTokenHash     string
	ReplacementTokenHash string
	RotatedAt            time.Time
	ReplacementExpiresAt time.Time
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
	IssueForSession(
		identityID string,
		sessionID string,
		issuedAt time.Time,
		sessionExpiresAt time.Time,
	) (string, int32, error)
}

type SessionDetails struct {
	ID         string
	ClientID   *string
	DeviceID   *string
	DeviceName *string
	Platform   *string
	AppVersion *string
	IPAddress  *string
	UserAgent  *string

	ExpiresAt  time.Time
	LastSeenAt *time.Time
	CreatedAt  time.Time
}

type SessionReader interface {
	ListActiveByIdentity(
		ctx context.Context,
		identityID string,
		now time.Time,
	) ([]SessionDetails, error)
}

type SessionRevocationTarget struct {
	SessionID        string
	SessionExpiresAt time.Time
}

type SessionRevocationTargetStore interface {
	FindRevocationTargetByRefreshTokenHash(
		ctx context.Context,
		refreshTokenHash string,
	) (SessionRevocationTarget, bool, error)
}

type SessionManagementRevocationTargetStore interface {
	FindRevocationTargetByIdentityAndSessionID(
		ctx context.Context,
		identityID string,
		sessionID string,
	) (SessionRevocationTarget, bool, error)
}

type SessionManagementRevocationStore interface {
	RevokeSession(
		ctx context.Context,
		identityID string,
		sessionID string,
		revokedAt time.Time,
	) error
}

type SessionAccessRevocationStore interface {
	MarkRevoked(
		ctx context.Context,
		sessionID string,
		ttl time.Duration,
	) error

	IsRevoked(
		ctx context.Context,
		sessionID string,
	) (bool, error)
}

type SessionAccessState struct {
	SessionExpiresAt time.Time
	Revoked          bool
}

type SessionAccessStateStore interface {
	FindSessionAccessState(
		ctx context.Context,
		sessionID string,
	) (SessionAccessState, bool, error)
}

type SessionRevocationStore interface {
	RevokeByRefreshTokenHash(
		ctx context.Context,
		refreshTokenHash string,
		revokedAt time.Time,
	) error
}

type AllSessionsRevocationTarget struct {
	IdentityID string
	Sessions   []SessionRevocationTarget
}

type AllSessionsRevocationTargetStore interface {
	FindAllSessionRevocationTargetsByRefreshTokenHash(
		ctx context.Context,
		refreshTokenHash string,
		now time.Time,
	) (AllSessionsRevocationTarget, bool, error)
}

type AllSessionsPersistentRevocationStore interface {
	RevokeSessions(
		ctx context.Context,
		identityID string,
		sessionIDs []string,
		revokedAt time.Time,
	) error
}

type AllSessionsRevocationStore interface {
	RevokeAllByRefreshTokenHash(
		ctx context.Context,
		refreshTokenHash string,
		revokedAt time.Time,
	) error
}
